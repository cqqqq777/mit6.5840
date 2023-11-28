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
		Entries:   make([]Entry, 1, 1024),
		LastIndex: 0,
	}
}

// make sure rf has get the lock!!!!!!

func (rf *Raft) lastLog() *Entry {
	return &rf.logs.Entries[rf.logs.LastIndex]
}

func (rf *Raft) lastLogIndex() int {
	return rf.lastLog().Index
}

func (rf *Raft) lastLogTerm() int {
	return rf.lastLog().Term
}

func (rf *Raft) prevLog() *Entry {
	if rf.lastLogIndex() == 0 {
		return rf.lastLog()
	}
	return &rf.logs.Entries[rf.logs.LastIndex-1]
}

func (rf *Raft) prevLogIndex() int {
	return rf.prevLog().Index
}

func (rf *Raft) prevLogTerm() int {
	return rf.prevLog().Term
}

func (rf *Raft) appendLogs(entries ...Entry) {
	rf.logs.Entries = append(rf.logs.Entries, entries...)
	rf.logs.LastIndex += len(entries)
}

func (rf *Raft) removeLogsAfter(index int) {
	rf.logs.Entries = rf.logs.Entries[:index]
	rf.logs.LastIndex = rf.logsLen() - 1
}

func (rf *Raft) removeLastLog() {
	rf.removeLogsAfter(rf.lastLogIndex())
}

func (rf *Raft) logsLen() int {
	return len(rf.logs.Entries)
}

func (rf *Raft) logAt(index int) *Entry {
	return &rf.logs.Entries[index]
}

func (rf *Raft) logSlice(index int) []Entry {
	return rf.logs.Entries[index:]
}
