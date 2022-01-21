# mutex
```
type Mutex struct {
	state int32
	sema  uint32
}
```

互斥锁的状态
+ mutexLocked — 表示互斥锁的锁定状态
+ mutexWoken — 表示从正常模式被从唤醒
+ mutexStarving — 当前的互斥锁进入饥饿状态
+ waitersCount — 当前互斥锁上等待的 Goroutine 个数

## mutex lock()
1. 首先如果当前锁处于初始化状态就直接用 CAS (atomic.CompareAndSwapInt32()) 方法尝试获取锁
```
if atomic.CompareAndSwapInt32(&m.state, 0, mutexLocked) {
		if race.Enabled {
			race.Acquire(unsafe.Pointer(m))
		}
		return
	}
```
2. 如果获取锁失败就进入 lockSlow()方法
3. 会首先判断当前能不能进入自旋状态，如果可以就进入自旋，最多自旋 4 次
```
if old&(mutexLocked|mutexStarving) == mutexLocked && runtime_canSpin(iter) {
			// Active spinning makes sense.
			// Try to set mutexWoken flag to inform Unlock
			// to not wake other blocked goroutines.
			if !awoke && old&mutexWoken == 0 && old>>mutexWaiterShift != 0 &&
				atomic.CompareAndSwapInt32(&m.state, old, old|mutexWoken) {
				awoke = true
			}
			runtime_doSpin()
			iter++
			old = m.state
			continue
		}
```
4. 自旋完成之后，就会去计算当前的锁的状态
5. 然后尝试通过 CAS 获取锁
6. 如果没有获取到就调用 runtime_SemacquireMutex 方法休眠当前 goroutine 并且尝试获取信号量
```
runtime_SemacquireMutex(&m.sema, queueLifo, 1)
```
7. goroutine 被唤醒之后会先判断当前是否处在饥饿状态，（如果当前 goroutine 超过 1ms 都没有获取到锁就会进饥饿模式）
```
if old&mutexStarving != 0 {}
```
8. 如果处在饥饿状态就会获得互斥锁，如果等待队列中只存在当前 Goroutine，互斥锁还会从饥饿模式中退出 
```
if old&mutexStarving != 0 {
    if old&(mutexLocked|mutexWoken) != 0 || old>>mutexWaiterShift == 0 {
        throw("sync: inconsistent mutex state")
    }
    delta := int32(mutexLocked - 1<<mutexWaiterShift)
    if !starving || old>>mutexWaiterShift == 1 {
        delta -= mutexStarving
    }
    atomic.AddInt32(&m.state, delta)
    break
}
```
9. 如果不在，就会设置唤醒和饥饿标记、重置迭代次数并重新执行获取锁的循环
```
awoke = true
iter = 0
```

## unlock()
1. 直接调用 atomic.AddInt32()进行解锁,成功直接结束.失败调用unlockSlow()函数.解锁一个没有锁定的互斥量会报运行时错误
2. 判断是否处于饥饿模式
3. 饥饿模式，走 handoff 流程，直接将锁交给下一个等待的 goroutine，注意这个时候不会从饥饿模式中退出
```
runtime_Semrelease(&m.sema, true, 1)
```
4. 正常模式下,如果当前没有等待者.或者 goroutine 已经被唤醒或者是处于锁定状态了，就直接返回
```
if old>>mutexWaiterShift == 0 || old&(mutexLocked|mutexWoken|mutexStarving) != 0 {
    return
}
```
5. 如果有等待者唤醒等待者并且移交锁的控制权
```
new = (old - 1<<mutexWaiterShift) | mutexWoken
if atomic.CompareAndSwapInt32(&m.state, old, new) {
    runtime_Semrelease(&m.sema, false, 1)
    return
}
old = m.state
```

# RWMutex
```
type RWMutex struct {
	w           Mutex  // 复用互斥锁
	writerSem   uint32 // 信号量，用于写等待读
	readerSem   uint32 // 信号量，用于读等待写
	readerCount int32  // 当前执行读的 goroutine 数量
	readerWait  int32  // 写操作被阻塞的准备读的 goroutine 的数量
}
```

## 读锁
### RLock()
```
if atomic.AddInt32(&rw.readerCount, 1) < 0 {
    // A writer is pending, wait for it.
    runtime_SemacquireMutex(&rw.readerSem, false, 0)
}
```
首先是读锁， atomic.AddInt32(&rw.readerCount, 1)  调用这个原子方法，对当前在读的数量加一，如果返回负数，那么说明当前有其他写锁，这时候就调用 runtime_SemacquireMutex  休眠 goroutine 等待被唤醒

### RUnlock()
```
func (rw *RWMutex) RUnlock() {
	if r := atomic.AddInt32(&rw.readerCount, -1); r < 0 {
		// Outlined slow-path to allow the fast-path to be inlined
		rw.rUnlockSlow(r)
	}
}
```
解锁的时候对正在读的操作减一，如果返回值小于 0 那么说明当前有在写的操作，这个时候调用 rUnlockSlow  进入慢速通道
```
func (rw *RWMutex) rUnlockSlow(r int32) {
	if r+1 == 0 || r+1 == -rwmutexMaxReaders {
		race.Enable()
		throw("sync: RUnlock of unlocked RWMutex")
	}
	// A writer is pending.
	if atomic.AddInt32(&rw.readerWait, -1) == 0 {
		// The last reader unblocks the writer.
		runtime_Semrelease(&rw.writerSem, false, 1)
	}
}
```
被阻塞的准备读的 goroutine 的数量减一，readerWait 为 0，就表示当前没有正在准备读的 goroutine 这时候调用 runtime_Semrelease  唤醒写操作

## 写锁
### Lock()
```
func (rw *RWMutex) Lock() {
	// First, resolve competition with other writers.
	rw.w.Lock()
	// Announce to readers there is a pending writer.
	r := atomic.AddInt32(&rw.readerCount, -rwmutexMaxReaders) + rwmutexMaxReaders
	// Wait for active readers.
	if r != 0 && atomic.AddInt32(&rw.readerWait, r) != 0 {
		runtime_SemacquireMutex(&rw.writerSem, false, 0)
	}
}
```
1. 首先调用互斥锁的 lock，获取到互斥锁之后，
2. atomic.AddInt32(&rw.readerCount, -rwmutexMaxReaders)  调用这个函数阻塞后续的读操作
3. 如果计算之后当前仍然有其他 goroutine 持有读锁，那么就调用 runtime_SemacquireMutex  休眠当前的 goroutine 等待所有的读操作完成

### UnLock()
```
func (rw *RWMutex) Unlock() {
	// Announce to readers there is no active writer.
	r := atomic.AddInt32(&rw.readerCount, rwmutexMaxReaders)
	if r >= rwmutexMaxReaders {
		race.Enable()
		throw("sync: Unlock of unlocked RWMutex")
	}
	// Unblock blocked readers, if any.
	for i := 0; i < int(r); i++ {
		runtime_Semrelease(&rw.readerSem, false, 0)
	}
}
```
解锁的操作，会先调用 atomic.AddInt32(&rw.readerCount, rwmutexMaxReaders)  将恢复之前写入的负数，然后根据当前有多少个读操作在等待，循环唤醒


