package raft

import "log"

// RequestVoteArgs
// example RequestVote RPC arguments structure.
// field names must start with capital letters!
type RequestVoteArgs struct {
	// Your data here (2A, 2B).
	Term         int // candidate's term
	CandidateID  int // candidate requesting vote
	LastLogIndex int // index of candidate's last log entry
	LastLogTerm  int // term of candidate's last log
}

// RequestVoteReply
// example RequestVote RPC reply structure.
// field names must start with capital letters!
type RequestVoteReply struct {
	// Your data here (2A).
	Term        int  // current term, for candidate to update itself
	VoteGranted bool // ture means candidate received vote
}

// RequestVote
// example RequestVote RPC handler.
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (2A, 2B).
	rf.lock()
	defer rf.unlock()

	if rf.currentTerm < args.Term {
		rf.becomeFollower(args.Term)
	}

	reply.Term = rf.currentTerm

	if args.Term < rf.currentTerm {
		return
	}

	lastLog := rf.lastLog()
	upToDate := lastLog.Term < args.LastLogTerm || (lastLog.Term == args.LastLogTerm && lastLog.Index <= args.LastLogIndex)
	if (rf.voteFor == -1 || rf.voteFor == args.CandidateID) && upToDate {
		log.Printf("%d vote for %d\n", rf.me, args.CandidateID)
		rf.voteFor = args.CandidateID
		reply.VoteGranted = true
		rf.resetElectionTime()
	}
}
