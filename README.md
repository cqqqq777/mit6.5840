## Lab2
**忠于论文，重中之重。**
https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf
所有的 server 都应该遵守的规则：
1. If commitIndex > lastApplied: increment lastApplied, apply log[lastApplied] to state machine (§5.3)
   **永远保证 lastApplied 大于等于 commitIndex**
2.   If RPC request or response contains term T > currentTerm: set currentTerm = T, convert to follower (§5.1)
     处理任何请求前，**先判断参数中的 term 是否大于自身**

根据第二条就有
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
```
### Lab2 A
lab2 A 要完成的是选举过程，发起选举的时机只有
1. 集群启动
2. follower 一段时间没收到心跳，成为 candidate 发起选举
3. candidate 选举超时后将 term+1 再次发起选举

要实现的就有以下功能
1. follower 维护一个选举计时器来判断自己是否需要开启一轮选举
   **If election timeout elapses without receiving AppendEntries RPC from current leader or granting vote to candidate: convert to candidate**
   选举计时器应该有三个时机会被重置
    - 收到一个合法的心跳（term 大于等于自己）
    - 给 candidate 投赞成票
    - 自己开启了一轮选举
      因为 candidate 应该也要维护一个选举超时的计时器，所以 follower 和 candidate 可以共用一个 ticker
```go
func (rf *Raft) ticker() {  
    for rf.killed() == false {  
       // Your code here (2A)  
       // Check if a leader election should be started.       rf.lock()  
  
       // follower and candidate check whether election timeout  
       if time.Now().After(rf.electTime) && rf.state != Leader {  
          log.Printf("%d become candidate\n", rf.me)  
          rf.resetElectionTime()  
          rf.becomeCandidate()  
       }  
       rf.unlock()  
       // pause for a random amount of time between 50 and 350  
       // milliseconds.       ms := 50 + (rand.Int63() % 300)  
       time.Sleep(time.Duration(ms) * time.Millisecond)  
    }  
}

// 因为测试函数限制了一秒内不能发出超过十次的心跳，所以选举超时时间应该适当延长，但不能太长导致 5s 内还没有选举出 leader
func (rf *Raft) resetElectionTime() {  
    t := time.Now()  
    timeOut := time.Duration(300+rand.Intn(300)) * time.Millisecond  
    rf.electTime = t.Add(timeOut)  
}
```


2.  leader 定期向集群内的所有节点发出心跳
    ![[raft_heartbeat.png]]
    当选后周期性的向每个节点发出心跳，即 Entries 为空的 AppendEntries RPC，防止集群中其他节点发起选举
```go
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
                // heartbeat is an one-way request  
                go peer.Call("Raft.AppendEntries", heartbeat, reply)  
             }  
          }  
       }  
       rf.unlock()  
       // send heartbeat RPCs every 100 millisecond  
       time.Sleep(time.Millisecond * 100)  
    }  
}
```
当 leader 变回 follower 时就发出信号关闭这个函数，避免其一直占内存
```go
func (rf *Raft) stopBroadcastHeartbeat() {  
    rf.cancelBroadcastHeart <- struct{}{}  
}
```

处理 AppendEntries 就可以根据是否携带日志来分为两个分支
```go
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
```

3. 选举过程
   当一段时间没收到合法心跳后就应该变为 candidate 开始新一轮选举
   下图详细说明了选举的过程
   ![[raft_election.png]]
   选举前置处理：
- 自增 term
- 给自己投票
- 重置 election timer
- 给每个节点发 RequestVote RPC
  后置处理：
- 超时前如果收到半数以上赞成票，成为 leader（注意如果收到了 term 小于自身的，视作过期的票，不计入总数中）
- 超时前收到 leader 发出的 AppendEntries RPC，直接变回 follower
- 超时的话再次开启选举
```go
func (rf *Raft) becomeCandidate() {  
    if rf.state == Leader {  
       panic("can't become to candidate from leader")  
    }  
    rf.state = Candidate  
    rf.startElection()  
}

func (rf *Raft) startElection() {  
    rf.currentTerm++  
    rf.voteFor = rf.me  
    rejectCount, grantCount := 0, 1  
    once := &sync.Once{} // 使用这个保证选举成功后只会执行一次成为 leader 的函数 
    lastLog := rf.lastLog()  
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

节点收到 RequestVote RPC 的处理逻辑
![[raft_request_vote.png]]
1. 如果 term 小于自身，投反对票
2. 如果自己没投票或已经投给此 candidate，并且 candidate 的 log 时期大于等于自己，投赞成票
```go
// RequestVote  
// example RequestVote RPC handler.
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply)
{  
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
```
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