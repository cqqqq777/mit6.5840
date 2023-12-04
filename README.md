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
4. 向每个几点发送 RequestVote RPC

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

### Lab2 C

### Lab2 D


## Lab3

## Lab4
