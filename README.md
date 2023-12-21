## Lab2

忠于论文，重中之重：https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf

### Lab2 A

Lab2 a 要完成的是选举的过程。

可以将选举分为发起选举、选举过程和处理投票请求三个部分来分别处理

#### 发起选举

根据论文，维护一个 election timeout 来判断是否需要发起一轮选举，补充在 ticker 函数中即可，重置选举超时器的时机在论文与[实验指导](https://thesquareplanet.com/blog/students-guide-to-raft/)中已经说明：

- 收到合法的 AppendEntries RPC
- 自身发起选举
- 给 candidate 投票

```go
// 在 raft 中添加 electTime 字段来实现
func (rf *Raft) ticker() {
    for rf.killed() == false {
       // Your code here (2A)
       // Check if a leader election should be started.
       rf.lock()

       // follower and candidate check whether election timeout
       if time.Now().After(rf.electTime) && rf.state != Leader {
          log.Printf("%d become candidate\n", rf.me)
          // 自身发起选举，重置 electTime
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

func (rf *Raft) resetElectionTime() {
	t := time.Now()
        // 因为 test 限制一秒内不能发出超过十次心跳，所以选举超时时间应该适当延长
	timeOut := time.Duration(300+rand.Intn(300)) * time.Millisecond
	rf.electTime = t.Add(timeOut)
}

func (rf *Raft) becomeCandidate() {
	if rf.state == Leader {
		panic("can't become to candidate from leader")
	}
	rf.state = Candidate
	rf.startElection()
}
```

#### 选举过程

根据图二中 candidate 的第一条规则实现 startElection()

1. 自增 term
2. 投票给自己
3. 重置 election timer
4. 向所有节点发送 RequestVote RPC

```go
func (rf *Raft) startElection() {
    rf.currentTerm++
    rf.voteFor = rf.me
    rejectCount, grantCount := 0, 1
    once := &sync.Once{}
    lastLog := rf.lastLog()
    // args 和 reply 都根据图二补充完整
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
```

candidate 还要根据每个节点的响应结果来判断选举结果，

- 如果有半数以上的合法赞成票，就成为 leader
    - 合法是指响应中 term 应该等于自身的 term，用于幂等去重
- 如果收到响应的 term 大于自身，变回 follower
- 选举超时就重新发起选举（这个在上面的 ticker 中就已经体现了）

使用 sync.Once 保证只会成为一次 leader

```go
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
```

当中变为 follower 比较简单，变为 leader 则需要周期性的发出心跳（图二中 leader 的规则一）

```go
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

// only candidate can become leader after it passed election
func (rf *Raft) becomeLeader() {
	if rf.state == Leader {
		return
	}
	if rf.state == Follower {
		panic("can't become to leader from follower")
	}
  	// 用于在 leader 退位后关闭发送心跳的线程，避免 goroutine 一直堆积
	rf.cancelBroadcastHeartbeat = make(chan struct{})
	rf.voteFor = -1
	rf.state = Leader
	go rf.heartbeatTicker()
}

func (rf *Raft) stopBroadcastHeartbeat() {
	// timer to avoid goroutine leak
	timer := time.After(time.Second * 3)
	select {
	case rf.cancelBroadcastHeartbeat <- struct{}{}:
	case <-timer:
	}
}
```

心跳在论文中的描述是不携带 entries 的 AppendEntries RPC，但是在 lab2 B 中的日志同步请求中，有可能收到任期一样但是拒绝同步的情况，这需要 leader 递归的向其发送前一条日志，这种情况其实可以放在心跳中来发送，减少 RPC 量但是增加了同步时延

```go
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
```

#### 处理投票请求

投票请求对应的是 RequestVote RPC，填充函数即可

处理逻辑在图二中已经详细说明：

1. 如果 term 小于自身，拒绝
2. 如果 voteFor 为空或者是 candidateId，并且 candidate 的日志跟自己一样或比自己更新，赞成
    1. 自己最新日志的 term 小于 candidate 的最新日志 term
    2. 自己最新日志的 term 与 candidate 的最新日志 term 相等并且自己最新日志的 index 小于等于 candidate 的最新日志 index

```go
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

    // If votedFor is null or candidateId, and candidate’s log is at
    // least as up-to-date as receiver’s log, grant vote
    lastLog := rf.lastLog()
    upToDate := lastLog.Term < args.LastLogTerm || (lastLog.Term == args.LastLogTerm && lastLog.Index <= args.LastLogIndex)
    if (rf.voteFor == -1 || rf.voteFor == args.CandidateID) && upToDate {
       log.Printf("%d vote for %d\n", rf.me, args.CandidateID)
       rf.voteFor = args.CandidateID
       reply.VoteGranted = true
       // 给别人投票了应该重置 election timer
       rf.resetElectionTime()
    }
}
```

最后在 Make 函数中初始化 raft 中的字段即可。

### Lab2 B

lab2 B 就是要实现 start 函数，并且要自己实现一个 applier 将提交的日志应用到状态机上（就是发到 Make 函数参数的那个管道里面），这对应了用户的写操作，根据论文的描述，一个成功的写操作的流程大致如下：

1. 节点收到客户端请求，判断自身身份，leader 则进入以下的步骤，否则直接返回
2. 生成预写日志，追加到预写日志数组末尾
3. 向每个节点发起 AppendEntries 的 RPC（提议）
4. follower 判断是否接受此条日志，返回 leader ack 响应
5. leader 发现多数 follower 都接受了此日志，则提交此日志并返回客户端响应，如果要实现强一致性那就要等此日志应用到状态机后再返回客户端响应（提交）

follower 在判断是否接受日志时，可能会有以下情况：

- **leader 的 term 滞后**：拒绝同步请求，告诉 leader 新 term
- **follower 的日志滞后**：拒绝同步请求，leader 发现响应 term 与自身一样后会递归向此 follower 发前一笔日志直至 follower 补全
- **follower 日志超前**：follower 移除超前的日志，与 leader 保持一致

为了提高 QPS，可以采取一些优化：

- 在 follower 发现日志滞后时，reply 中增加一些字段来让 leader 可以一次性把缺失的日志全部发过来，这个必要性不大，实际情况滞后的概率较小，不优化也可以过测试函数。
- follower 日志滞后，后面再次发出同步日志请求时可以随心跳一起发送给用户。

先实现 start 函数，追加到自己日志末尾后向每一个节点发送 AppendEntries RPC。

```go
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
```

AppendEntries 分了两种，一种是心跳，一种是日志追加，心跳是单向的不用管响应值，但需要处理日志追加请求的响应。

当发现不是因为任期问题而被拒绝时，就递归的向其发送前一条日志，发送的时机为心跳的时候。

```go
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
```

lab2 B 中还要完成的一点是将被大多数节点同步的日志应用到状态机中，论文图二中 leader 应该遵守的最后一条规则说明了什么样的日志应该被提交。

```go
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

// apply 根据 lab 的提示，使用一个条件变量来控制 applier
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
```

leader 要做的大概就这些了，还有就是 follower 收到 AppendEntries 时的处理。

分为了两种，未携带日志的心跳，携带日志的同步请求

```go
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
			rf.persist()
		}

		// Append any new entries not already in the log
		log.Printf("%d append %d entries, begin at index: %d, term: %d\n", rf.me, len(args.Entries)-i, entry.Index, entry.Term)
		rf.appendLogs(args.Entries[i:]...)
		rf.persist()
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
```

### Lab2 C

Lab2 C 要做的是持久化，其实我感觉比前两个都要简单，在论文的图二中说明了需要持久化的三个字段，分别是 currentTerm、voteFor 和 log[]，按找给的代码示例，替换成这三个字段，每次这三个字段变化的时候，都持久化即可。

```go
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
```

### Lab2 D

TODO

## Lab3

TODO

## Lab4

TODO
