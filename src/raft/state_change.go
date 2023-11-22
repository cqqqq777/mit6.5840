package raft

import (
	"log"
	"runtime"
	"time"
)

// only candidate can become leader after it passed election
func (rf *Raft) becomeLeader() {
	if rf.state == Leader {
		return
	}
	if rf.state == Follower {
		panic("can't become to leader from follower")
	}
	rf.cancelBroadcastHeart = make(chan struct{})
	rf.voteFor = -1
	rf.state = Leader
	go rf.heartbeatTicker()
}

// leader should broadcast heartbeat periodically
func (rf *Raft) heartbeatTicker() {
	for {
		rf.lock()
		select {
		case <-rf.cancelBroadcastHeart:
			rf.unlock()
			runtime.Goexit()
		default:
			heartbeat := &AppendEntriesArgs{
				Term:         rf.currentTerm,
				LeaderId:     rf.me,
				PrevLogIndex: rf.prevLogIndex(),
				PrevLogTerm:  rf.prevLogTerm(),
				LeaderCommit: rf.commitIndex,
				Entries:      nil,
			}
			for id, peer := range rf.peers {
				if id != rf.me {
					reply := &AppendEntriesReply{}
					go peer.Call("Raft.AppendEntries", heartbeat, reply)
				}
			}
		}
		rf.unlock()
		// send heartbeat RPCs every 100 millisecond
		time.Sleep(time.Millisecond * 100)
	}
}

func (rf *Raft) becomeCandidate() {
	if rf.state == Leader {
		panic("can't become to candidate from leader")
	}
	rf.state = Candidate
	rf.startElection()
}

func (rf *Raft) becomeFollower(term int) {
	if rf.state == Leader {
		// stop leader's ticker
		go rf.stopBroadcastHeartbeat()
	}
	rf.voteFor = -1
	rf.currentTerm = term
	if rf.state != Follower {
		log.Printf("%d become follower\n", rf.me)
		rf.state = Follower
	}
}

func (rf *Raft) stopBroadcastHeartbeat() {
	rf.cancelBroadcastHeart <- struct{}{}
}
