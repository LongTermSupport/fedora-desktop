// Package badgo starts goroutines that signal no end, beside ones whose owner can wait.
package badgo

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

type received struct{ err error }

// Link is a link whose reader is started unowned.
type Link = link

type link struct {
	recv func() error
	msgs chan received
	quit chan struct{}
	done chan struct{}
	wg   sync.WaitGroup
}

// Run is the originating instance: read is stopped by quit, which only the success path
// closes, and nothing can wait for read, so an early return leaves it blocked on its send.
func (l *link) Run(ok bool) error {
	go l.read() // want `goroutine signals no end, so no owner can wait for it`
	if !ok {
		return errors.New("no launch")
	}
	defer close(l.quit)
	return nil
}

func (l *link) read() {
	for {
		err := l.recv()
		select {
		case l.msgs <- received{err: err}:
		case <-l.quit:
			return
		}
	}
}

func Drains(ch <-chan int) {
	go func() { // want `goroutine signals no end, so no owner can wait for it`
		for range ch {
		}
	}()
}

// Sorts runs a function of another package, whose end nobody here can see.
func Sorts(xs []int) {
	go sort.Ints(xs) // want `goroutine signals no end, so no owner can wait for it`
}

// SkipsItsSignal sends its end last, but an early return skips the send.
func SkipsItsSignal(stop bool, done chan<- struct{}) {
	go func() { // want `goroutine signals no end, so no owner can wait for it`
		if stop {
			return
		}
		done <- struct{}{}
	}()
}

// ThroughAValue starts a function value, whose body is not known.
func ThroughAValue(l *link) {
	f := l.read
	go f() // want `goroutine signals no end, so no owner can wait for it`
}

// DefersOther defers something that is not an end signal.
func DefersOther() {
	go func() { // want `goroutine signals no end, so no owner can wait for it`
		defer fmt.Println("gone")
		select {}
	}()
}

// ForwardsToUnowned wraps a goroutine body that signals nothing.
func ForwardsToUnowned(l *link) {
	go func() { l.read() }() // want `goroutine signals no end, so no owner can wait for it`
}

func ClosesDone(done chan struct{}) {
	go func() {
		defer close(done)
		select {}
	}()
	<-done
}

func CountsDown(wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()
	}()
	wg.Wait()
}

func SendsItsResult(work func() error) error {
	done := make(chan error, 1)
	go func() { done <- work() }()
	return <-done
}

func (l *link) owned() {
	defer close(l.done)
	l.read()
}

func StartsOwned(l *link) {
	go l.owned()
	go func() { l.owned() }()
	<-l.done
}

func StartsThroughAWaitGroup(l *link) {
	l.wg.Go(l.read)
	l.wg.Wait()
}

func DefersALiteral(done chan struct{}) {
	go func() {
		defer func() { close(done) }()
	}()
	<-done
}

func ClosesLast(done chan struct{}) {
	go func() {
		fmt.Println("work")
		close(done)
	}()
	<-done
}

func generic[T any](done chan T) {
	defer close(done)
}

func StartsGeneric(done chan int) {
	go generic(done)
	<-done
}
