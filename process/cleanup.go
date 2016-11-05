package process

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

// Cleanup is a best-effort to remove all temporary files
// created by process. Users can call it manually to remove them.
func Cleanup() {
	matches, err := filepath.Glob(filepath.Join(os.TempDir(), fmt.Sprintf("%s.*", prefix)))
	if err != nil {
		log.Fatal(err)
	}
	for _, f := range matches {
		os.Remove(f)
	}
}

func init() {
	c := make(chan os.Signal, 1)
	signal.Notify(c,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGHUP,
		syscall.SIGQUIT)
	go func() {
		s := <-c
		Cleanup()
		fmt.Fprintln(os.Stderr, s)
		os.Exit(2)
	}()
	go func() {
		if err := recover(); err != nil {
			Cleanup()
			panic(err)
		}
	}()
}
