package raft

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
//   start agreement on a new log entry
// rf.GetState() (term, isLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import (
	"bytes"
	"log"
	//	"bytes"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"6.5840/labgob"
	"6.5840/labrpc"
)

// ApplyMsg
// as each Raft peer becomes aware that successive log entries are
// committed, the peer should send an ApplyMsg to the service (or
// tester) on the same server, via the applyCh passed to Make(). set
// CommandValid to true to indicate that the ApplyMsg contains a newly
// committed log entry.
//
// in part 2D you'll want to send other kinds of messages (e.g.,
// snapshots) on the applyCh, but set CommandValid to false for these
// other uses.
type ApplyMsg struct {
	CommandValid bool
	Command      interface{}
	CommandIndex int

	// For 2D:
	SnapshotValid bool
	Snapshot      []byte
	SnapshotTerm  int
	SnapshotIndex int
}

type State uint8

const (
	Leader State = iota
	Follower
	Candidate
)

// Raft
// A Go object implementing a single Raft peer.
type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *Persister          // Object to hold this peer's persisted state
	me        int                 // this peer's index into peers[]
	dead      int32               // set by Kill()

	// Your data here (2A, 2B, 2C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.
	currentTerm int  // latest term server has seen (initialized to 0 on first boot, increases monotonically)
	voteFor     int  // candidateID that received vote in current term(-1 if none)
	logs        RLog // log entries, first index is 1

	// Volatile state on all servers
	commitIndex int
	lastApplied int

	state State // Leader, Follower or Candidate

	// for follower and candidate
	electTime time.Time

	// for leader
	// if the current raft instance is not a leader, they will be set to nil
	nextIndex                []int // for each server, index of the next log entry to send to that server (initialized to leader last log index + 1)
	matchIndex               []int // for each server, index of the highest log entry known to be replicated on server (initialized to 0, increases monotonically)
	cancelBroadcastHeartbeat chan struct{}

	applyCh   chan ApplyMsg
	applyCond *sync.Cond
}

func (rf *Raft) lock() {
	rf.mu.Lock()
}

func (rf *Raft) unlock() {
	rf.mu.Unlock()
}

// GetState
// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {
	rf.lock()
	defer rf.unlock()
	return rf.currentTerm, !rf.killed() && (rf.state == Leader)
}

// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
// before you've implemented snapshots, you should pass nil as the
// second argument to persister.Save().
// after you've implemented snapshots, pass the current snapshot
// (or nil if there's not yet a snapshot).
func (rf *Raft) persist() {
	// Your code here (2C).
	// Example:
	log.Printf("%d persist, term: %d, voteFor: %d, lastLog: term:%d, index: %d \n", rf.me, rf.currentTerm, rf.voteFor, rf.lastLog().Term, rf.lastLog().Index)
	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	e.Encode(rf.currentTerm)
	e.Encode(rf.voteFor)
	e.Encode(rf.logs)
	raftState := w.Bytes()
	rf.persister.Save(raftState, nil)
}

// restore previously persisted state.
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	// Your code here (2C).
	// Example:
	r := bytes.NewBuffer(data)
	d := labgob.NewDecoder(r)
	var currentTerm int
	var voteFor int
	var logs RLog
	if d.Decode(&currentTerm) != nil ||
		d.Decode(&voteFor) != nil ||
		d.Decode(&logs) != nil {
		log.Fatalf("%d read persist failed\n", rf.me)
	} else {
		rf.currentTerm = currentTerm
		rf.voteFor = voteFor
		rf.logs = logs
	}
}

// Snapshot
// the service says it has created a snapshot that has
// all info up to and including index. this means the
// service no longer needs the log through (and including)
// that index. Raft should now trim its log as much as possible.
func (rf *Raft) Snapshot(index int, snapshot []byte) {
	// Your code here (2D).

}

// Start
// the service using Raft (e.g. a k/v server) wants to start
// agreement on the next command to be appended to Raft's log. if this
// server isn't the leader, returns false. otherwise start the
// agreement and return immediately. there is no guarantee that this
// command will ever be committed to the Raft log, since the leader
// may fail or lose an election. even if the Raft instance has been killed,
// this function should return gracefully.
//
// the first return value is the index that the command will appear at
// if it's ever committed. the second return value is the current
// term. the third return value is true if this server believes it is
// the leader.
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	index := -1
	term := -1
	isLeader := true
	// Your code here (2B).

	// check whether the rf is a leader
	term, isLeader = rf.GetState()
	if !isLeader {
		return index, term, isLeader
	}

	rf.lock()
	defer rf.unlock()

	//lastLog := rf.lastLog()

	// appendLogs a new entry after entries array
	entry := Entry{
		Term:    rf.currentTerm,
		Index:   rf.logs.LastIndex + 1,
		Command: command,
	}
	rf.appendLogs(entry)
	rf.persist()
	log.Printf("leader %d append entry, index: %d, term: %d\n", rf.me, entry.Index, entry.Term)

	lastLog := rf.lastLog()
	// send AppendEntries RPC to every follower
	for peer := range rf.peers {
		if peer != rf.me {
			//  If last log index ≥ nextIndex for a follower: send
			// AppendEntries RPC with log entries starting at nextIndex
			if lastLog.Index >= rf.nextIndex[peer] {
				nextIdx := rf.nextIndex[peer]
				if nextIdx <= 0 {
					nextIdx = 1
				}
				if lastLog.Index+1 < nextIdx {
					nextIdx = lastLog.Index
				}
				prevLog := rf.logAt(nextIdx - 1)
				args := &AppendEntriesArgs{
					Term:         rf.currentTerm,
					LeaderId:     rf.me,
					PrevLogIndex: prevLog.Index,
					PrevLogTerm:  prevLog.Term,
					LeaderCommit: rf.commitIndex,
					Entries:      make([]Entry, lastLog.Index-nextIdx+1),
				}
				copy(args.Entries, rf.logSlice(nextIdx))
				log.Printf("leader %d send %d entries to %d, start at index: %d\n", rf.me, len(args.Entries), peer, nextIdx)
				go rf.sendAppendEntries(peer, args)
			}
		}
	}

	index, term = entry.Index, entry.Term
	return index, term, isLeader
}

// Kill
// the tester doesn't halt goroutines created by Raft after each test,
// but it does call the Kill() method. your code can use killed() to
// check whether Kill() has been called. the use of atomic avoids the
// need for a lock.
//
// the issue is that long-running goroutines use memory and may chew
// up CPU time, perhaps causing later tests to fail and generating
// confusing debug output. any goroutine with a long-running loop
// should call killed() to check whether it should stop.
func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
	// Your code here, if desired.
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}

func (rf *Raft) ticker() {
	for rf.killed() == false {
		// Your code here (2A)
		// Check if a leader election should be started.
		rf.lock()

		// follower and candidate check whether election timeout
		if time.Now().After(rf.electTime) && rf.state != Leader {
			log.Printf("%d become candidate\n", rf.me)
			rf.resetElectionTime()
			rf.becomeCandidate()
		}
		rf.unlock()
		// pause for a random amount of time between 50 and 350
		// milliseconds.
		ms := 50 + (rand.Int63() % 300)
		time.Sleep(time.Duration(ms) * time.Millisecond)
	}
}

func (rf *Raft) apply() {
	rf.applyCond.Signal()
}

func (rf *Raft) applier() {
	rf.lock()
	defer rf.unlock()

	for !rf.killed() {
		// If commitIndex > lastApplied: increment lastApplied, apply
		// log[lastApplied] to state machine
		if rf.commitIndex > rf.lastApplied && rf.lastLogIndex() > rf.lastApplied {
			rf.lastApplied++
			applyMsg := ApplyMsg{
				CommandValid: true,
				Command:      rf.logAt(rf.lastApplied).Command,
				CommandIndex: rf.lastApplied,
			}
			log.Printf("%d applied index: %d\n", rf.me, rf.lastApplied)
			// to avoid channel block
			rf.unlock()
			rf.applyCh <- applyMsg
			rf.lock()
		} else {
			rf.applyCond.Wait()
		}
	}
}

// Make
// the service or tester wants to create a Raft server. the ports
// of all the Raft servers (including this one) are in peers[]. this
// server's port is peers[me]. all the servers' peers[] arrays
// have the same order. persister is a place for this server to
// save its persistent state, and also initially holds the most
// recent saved state, if any. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
// Make() must return quickly, so it should start goroutines
// for any long-running work.
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me

	// Your initialization code here (2A, 2B, 2C).
	rf.state = Follower
	rf.logs = NewRLog()
	rf.voteFor = -1
	rf.resetElectionTime()

	rf.commitIndex = 0
	rf.lastApplied = 0
	rf.nextIndex = make([]int, len(rf.peers))
	rf.matchIndex = make([]int, len(rf.peers))

	rf.applyCond = sync.NewCond(&rf.mu)
	rf.applyCh = applyCh

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	// start ticker goroutine to start elections
	go rf.ticker()

	go rf.applier()

	return rf
}
