package raft

// Entry
// each entry contains command
// for state machine, and term when entry
// was received by leader (first index is 1)
type Entry struct {
	Term    int
	Index   int
	Command interface{}
}

type RLog struct {
	Entries   []Entry
	LastIndex int
}

func NewRLog() *RLog {
	return &RLog{
		Entries:   make([]Entry, 1024),
		LastIndex: 1,
	}
}

// make sure rf has get the lock

func (rf *Raft) lastLog() Entry {
	return rf.logs.Entries[rf.logs.LastIndex]
}

func (rf *Raft) lastLogIndex() int {
	return rf.lastLog().Index
}

func (rf *Raft) lastLogTerm() int {
	return rf.lastLog().Term
}

func (rf *Raft) prevLog() Entry {
	return rf.logs.Entries[rf.logs.LastIndex-1]
}

func (rf *Raft) prevLogIndex() int {
	return rf.prevLog().Index
}

func (rf *Raft) prevLogTerm() int {
	return rf.prevLog().Term
}
