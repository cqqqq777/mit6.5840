package raft

import (
	"log"
	"math/rand"
	"sync"
	"time"
)

func (rf *Raft) resetElectionTime() {
	t := time.Now()
	timeOut := time.Duration(300+rand.Intn(300)) * time.Millisecond
	rf.electTime = t.Add(timeOut)
}

func (rf *Raft) startElection() {
	rf.currentTerm++
	rf.voteFor = rf.me
	rf.persist()
	rejectCount, grantCount := 0, 1
	once := &sync.Once{}
	lastLog := rf.lastLog()
	args := &RequestVoteArgs{
		Term:         rf.currentTerm,
		CandidateID:  rf.me,
		LastLogIndex: lastLog.Index,
		LastLogTerm:  lastLog.Term,
	}
	for i := 0; i < len(rf.peers); i++ {
		if i != rf.me {
			reply := &RequestVoteReply{}
			go rf.sendRequestVote(i, args, reply, &rejectCount, &grantCount, once)
		}
	}
}

// example code to send a RequestVote RPC to a server.
// server is the index of the target server in rf.peers[].
// expects RPC arguments in args.
// fills in *reply with RPC reply, so caller should
// pass &reply.
// the types of the args and reply passed to Call() must be
// the same as the types of the arguments declared in the
// handler function (including whether they are pointers).
//
// The labrpc package simulates a lossy network, in which servers
// may be unreachable, and in which requests and replies may be lost.
// Call() sends a request and waits for a reply. If a reply arrives
// within a timeout interval, Call() returns true; otherwise
// Call() returns false. Thus Call() may not return for a while.
// A false return can be caused by a dead server, a live server that
// can't be reached, a lost request, or a lost reply.
//
// Call() is guaranteed to return (perhaps after a delay) *except* if the
// handler function on the server side does not return.  Thus there
// is no need to implement your own timeouts around Call().
//
// look at the comments in ../labrpc/labrpc.go for more details.
//
// if you're having trouble getting RPC to work, check that you've
// capitalized all field names in structs passed over RPC, and
// that the caller passes the address of the reply struct with &, not
// the struct itself.
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply, rejectCount, grantCount *int, once *sync.Once) {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	if !ok {
		return
	}
	rf.lock()
	defer rf.unlock()
	if reply.Term > rf.currentTerm {
		log.Printf("candidate %d update term\n", rf.me)
		rf.becomeFollower(reply.Term)
		return
	}
	// this vote has timeout
	if reply.Term < rf.currentTerm {
		log.Printf("[%d]: %d's vote out of date\n", rf.me, server)
		return
	}
	if !reply.VoteGranted {
		*rejectCount++
		if *rejectCount > len(rf.peers)/2 && rf.state == Candidate && rf.currentTerm == args.Term {
			log.Printf("%d received more than half of reject vote\n", rf.me)
			rf.becomeFollower(rf.currentTerm)
		}
		return
	}
	*grantCount++
	if *grantCount > len(rf.peers)/2 && rf.state == Candidate && rf.currentTerm == args.Term {
		log.Printf("%d become leader\n", rf.me)
		once.Do(rf.becomeLeader)
	}
}
