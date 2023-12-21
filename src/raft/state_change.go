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
	rf.cancelBroadcastHeartbeat = make(chan struct{})
	rf.voteFor = -1
	rf.persist()
	rf.state = Leader
	go rf.heartbeatTicker()
}

// leader should broadcast heartbeat periodically
func (rf *Raft) heartbeatTicker() {
	for {
		rf.lock()
		select {
		case <-rf.cancelBroadcastHeartbeat:
			rf.unlock()
			runtime.Goexit()
		default:
			lastLog := rf.lastLog()
			for peer := range rf.peers {
				if peer != rf.me {
					nextIndex := rf.nextIndex[peer]
					if nextIndex <= 0 {
						nextIndex = 1
					}
					if lastLog.Index+1 < nextIndex {
						nextIndex = lastLog.Index
					}
					prevLog := rf.logAt(nextIndex - 1)
					heartbeat := &AppendEntriesArgs{
						Term:         rf.currentTerm,
						LeaderId:     rf.me,
						PrevLogIndex: prevLog.Index,
						PrevLogTerm:  prevLog.Term,
						LeaderCommit: rf.commitIndex,
						Entries:      make([]Entry, lastLog.Index-nextIndex+1),
					}
					copy(heartbeat.Entries, rf.logSlice(nextIndex))
					if len(heartbeat.Entries) != 0 {
						log.Printf("leader %d send %d entries to %d, start at index: %d (heartbeat)\n", rf.me, len(heartbeat.Entries), peer, nextIndex)
					}
					go rf.sendAppendEntries(peer, heartbeat)
				}
			}
		}
		rf.unlock()
		// send heartbeat RPCs every 100 millisecond
		time.Sleep(time.Millisecond * 100)
	}
}

func (rf *Raft) tryCommit() {
	if rf.state != Leader {
		return
	}
	// If there exists an N such that N > commitIndex, a majority
	// of matchIndex[i] ≥ N, and log[N].term == currentTerm:
	// set commitIndex = N
	for n := rf.commitIndex + 1; n <= rf.lastLogIndex(); n++ {
		if rf.logAt(n).Term != rf.currentTerm {
			continue
		}
		counter := 1
		for peer := range rf.peers {
			if peer != rf.me && rf.matchIndex[peer] >= n {
				counter++
			}
			if counter > len(rf.peers)/2 {
				rf.commitIndex = n
				log.Printf("leader %d commit log at index: %d\n", rf.me, n)
				rf.apply()
				break
			}
		}
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
	rf.persist()
	if rf.state != Follower {
		log.Printf("%d become follower\n", rf.me)
		rf.state = Follower
	}
}

func (rf *Raft) stopBroadcastHeartbeat() {
	// timer to avoid goroutine leak
	timer := time.After(time.Second * 3)
	select {
	case rf.cancelBroadcastHeartbeat <- struct{}{}:
	case <-timer:
	}
}
