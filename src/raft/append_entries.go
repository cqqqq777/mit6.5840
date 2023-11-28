package raft

import (
	"log"
)

type AppendEntriesArgs struct {
	Term         int     // leader's term
	LeaderId     int     // leader's id, so follower can redirect requests
	PrevLogIndex int     // index of log entry immediately preceding new ones
	PrevLogTerm  int     // term of prevLogIndex entry
	LeaderCommit int     // leader's commitIndex
	Entries      []Entry // log entries to store(empty for heartbeat)
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

	// Reply false if term < currentTerm
	if rf.currentTerm > args.Term {
		reply.Term = rf.currentTerm
		return
	}

	// convert to follower
	if args.Term > rf.currentTerm {
		rf.becomeFollower(args.Term)
	}
	reply.Term = rf.currentTerm

	rf.resetElectionTime()
	if len(args.Entries) == 0 {
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
		rf.apply()
	}
}

func (rf *Raft) handelAppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	lastLog := rf.lastLog()

	//  Reply false if log don’t contain an entry at prevLogIndex whose term matches prevLogTerm
	if lastLog.Index < args.PrevLogIndex || rf.logAt(args.PrevLogIndex).Term != args.PrevLogTerm {
		log.Printf("%d logs are not contain an entry at Index: %d, Term: %d\n", rf.me, args.PrevLogIndex, args.PrevLogTerm)
		return
	}

	// If an existing entry conflicts with a new one (same index
	// but different terms), delete the existing entry and all that
	// follow it
	idx := args.PrevLogIndex
	l := rf.logsLen()
	for i, entry := range args.Entries {
		idx++
		if idx < l {
			if rf.logAt(idx).Term == entry.Term {
				continue
			}
			rf.removeLogsAfter(idx)
		}

		// Append any new entries not already in the log
		log.Printf("%d append %d entries, begin at index: %d, term: %d\n", rf.me, len(args.Entries)-i, entry.Index, entry.Term)
		rf.appendLogs(args.Entries[i:]...)
		break
	}
	reply.Success = true

	if args.LeaderCommit > rf.commitIndex {
		rf.commitIndex = min(args.LeaderCommit, rf.lastLogIndex())
		rf.apply()
	}

	// avoid election time out
	rf.resetElectionTime()
}

// leader sends AppendEntries RPC to peers
func (rf *Raft) sendAppendEntries(peer int, args *AppendEntriesArgs) {
	reply := &AppendEntriesReply{}
	if !rf.peers[peer].Call("Raft.AppendEntries", args, reply) {
		return
	}
	rf.lock()
	defer rf.unlock()

	if rf.state != Leader || reply.Term < rf.currentTerm {
		return
	}

	if reply.Term > rf.currentTerm {
		rf.becomeFollower(reply.Term)
		return
	}

	if args.Term == rf.currentTerm {
		if reply.Success {
			// update nextIndex and matchIndex
			match := args.PrevLogIndex + len(args.Entries)
			next := match + 1
			rf.nextIndex[peer] = max(next, rf.nextIndex[peer])
			rf.matchIndex[peer] = max(match, rf.matchIndex[peer])
			log.Printf("leader %d AppendEntries for %d successfully, match: %d, next: %d\n", rf.me, peer, match, next)
		} else if rf.nextIndex[peer] > 1 && len(args.Entries) != 0 {
			rf.nextIndex[peer]--
		}
		rf.tryCommit()
	}
}
