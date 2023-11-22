package raft

type AppendEntriesArgs struct {
	Term         int      // leader's term
	LeaderId     int      // leader's id, so follower can redirect requests
	PrevLogIndex int      // index of log entry immediately preceding new ones
	PrevLogTerm  int      // term of prevLogIndex entry
	LeaderCommit int      // leader's commitIndex
	Entries      []*Entry // log entries to store(empty for heartbeat)
}

type AppendEntriesReply struct {
	Term    int  // currentTerm, for leader to update itself
	Success bool // true if follower contained entry matching prevLogIndex and prevLogTerm
}

// AppendEntries
// Invoked by leader to replicate log entries
// also used as heartbeat
func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.lock()
	defer rf.unlock()

	if rf.currentTerm > args.Term {
		reply.Term = rf.currentTerm
		return
	}

	rf.resetElectionTime()
	if args.Entries == nil {
		if args.Term > rf.currentTerm {
			rf.becomeFollower(args.Term)
		}
		// handle heartbeat
		rf.handleHeartbeat(args)
		return
	}

	// handle AppendEntries RPC
	rf.handelAppendEntries(args, reply)
}

func (rf *Raft) handleHeartbeat(args *AppendEntriesArgs) {
	if args.LeaderCommit > rf.commitIndex {
		rf.commitIndex = min(args.LeaderCommit, rf.lastLogIndex())
	}
}

func (rf *Raft) handelAppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {

}
