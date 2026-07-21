// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"bufio"
	"context"
	"os"
	"os/signal"
	"sync"

	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

// ContextWithInterrupt returns a child context canceled by the first Ctrl-C.
// Use this for commands where an interrupt means cancel the current operation.
// Not used for recipe run commands, which have a separate stop/cancel mechanism.
func ContextWithInterrupt(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, os.Interrupt)
}

// RecipeCommand represents a command that can be sent to a running recipe.
type RecipeCommand int

const (
	STOP RecipeCommand = iota
	CANCEL
)

var (
	// cmdManager is the RecipeCommandManager instance.
	cmdManager *RecipeCommandManager
	// initOnce ensures cmdManager is initialized only once.
	initOnce sync.Once
	// byteToCommand maps std input to RecipeCommands.
	byteToCommand = map[byte]RecipeCommand{
		's': STOP,
		'c': CANCEL,
		'q': CANCEL,
	}
)

// RecipeCommandManager manages recipe commands and notifies registered callbacks
// when commands are received.
type RecipeCommandManager struct {
	// callbacks contains the registered callbacks.
	callbacks []callbackEntry
	// receivers push commands to commandCh.
	receivers []receiver
	// commandCh is the channel on which commands are received.
	commandCh chan RecipeCommand
	// bufferedCommands holds commands received before any callbacks are registered.
	// This ensures that a late-registered callback still receives the required commands.
	bufferedCommands []RecipeCommand
	// mu protects access to callbacks.
	mu sync.Mutex
}

// callbackEntry is a registered callback and its associated client.
type callbackEntry struct {
	fn     func(apapproto.ApapClient, RecipeCommand)
	client apapproto.ApapClient
}

// InitRecipeCommandManager initializes the package level RecipeCommandManager.
func InitRecipeCommandManager() {
	initOnce.Do(func() {
		cmdManager = newDefaultRecipeCommandManager()
		cmdManager.start()
	})
}

// RegisterCallback registers a callback function to be called when a RecipeCommand is received.
// cmdManager is initialized if required.
func RegisterCallback(cb func(apapproto.ApapClient, RecipeCommand), client apapproto.ApapClient) {
	// Ensure the manager is initialized.
	InitRecipeCommandManager()

	cmdManager.registerCallback(cb, client)
}

// newDefaultRecipeCommandManager creates a RecipeCommandManager with default receivers.
func newDefaultRecipeCommandManager() *RecipeCommandManager {
	return newRecipeCommandManager(
		newStdinReceiver(stdinReader{}, byteToCommand),
		newInterruptReceiver(osSignalNotifier{}, STOP, CANCEL),
	)
}

// newRecipeCommandManager creates a new RecipeCommandManager with the given receivers.
func newRecipeCommandManager(receivers ...receiver) *RecipeCommandManager {
	return &RecipeCommandManager{
		receivers: receivers,
		commandCh: make(chan RecipeCommand, 1),
	}
}

// start starts all receivers and begins handling commands.
func (m *RecipeCommandManager) start() {
	for _, receiver := range m.receivers {
		if receiver != nil {
			receiver.start(m.commandCh)
		}
	}
	go func() {
		for cmd := range m.commandCh {
			m.handleCommand(cmd)
		}
	}()
}

// registerCallback adds a callback to the manager.
func (m *RecipeCommandManager) registerCallback(cb func(apapproto.ApapClient, RecipeCommand), client apapproto.ApapClient) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.callbacks = append(m.callbacks, callbackEntry{fn: cb, client: client})

	// Invoke the callback for any buffered commands.
	for _, cmd := range m.bufferedCommands {
		cb(client, cmd)
	}
	m.bufferedCommands = nil
}

// handleCommand invokes all registered callbacks with the given command.
func (m *RecipeCommandManager) handleCommand(cmd RecipeCommand) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.callbacks) == 0 {
		m.bufferedCommands = append(m.bufferedCommands, cmd)
		return
	}

	for _, cb := range m.callbacks {
		cb.fn(cb.client, cmd)
	}
}

// byteReader defines an interface for reading bytes.
type byteReader interface {
	Read(p []byte) (n int, err error)
}

// stdinReader implements byteReader using os.Stdin.
type stdinReader struct{}

func (stdinReader) Read(p []byte) (int, error) {
	return os.Stdin.Read(p)
}

// signalNotifier defines an interface for signal notification.
type signalNotifier interface {
	Notify(chan<- os.Signal, ...os.Signal)
	Stop(chan<- os.Signal)
	Reset(...os.Signal)
	Signal(os.Signal) error
}

// osSignalNotifier implements signalNotifier using the signal package.
type osSignalNotifier struct{}

func (osSignalNotifier) Notify(c chan<- os.Signal, sig ...os.Signal) {
	signal.Notify(c, sig...)
}

func (osSignalNotifier) Stop(c chan<- os.Signal) {
	signal.Stop(c)
}

func (osSignalNotifier) Reset(sig ...os.Signal) {
	signal.Reset(sig...)
}

func (osSignalNotifier) Signal(sig os.Signal) error {
	p, err := os.FindProcess(os.Getpid())
	if err != nil {
		return err
	}
	return p.Signal(sig)
}

// receiver pushes recipe commands onto commandCh.
type receiver interface {
	start(chan<- RecipeCommand)
}

// stdinReceiver implements a receiver that reads from stdin.
type stdinReceiver struct {
	reader  byteReader
	byteMap map[byte]RecipeCommand
}

// newStdinReceiver creates a new stdinReceiver.
func newStdinReceiver(reader byteReader, byteMap map[byte]RecipeCommand) receiver {
	return &stdinReceiver{reader: reader, byteMap: byteMap}
}

// start reads from stdin and pushes commands onto out.
func (r *stdinReceiver) start(out chan<- RecipeCommand) {
	go func() {
		scanner := bufio.NewReader(r.reader)
		for {
			b, err := scanner.ReadByte()
			if err != nil {
				return
			}
			if cmd, ok := r.byteMap[b]; ok {
				out <- cmd
			}
		}
	}()
}

// interruptReceiver implements a receiver that listens for OS interrupts.
type interruptReceiver struct {
	notifier signalNotifier
	cmds     []RecipeCommand
}

// newInterruptReceiver creates a new interruptReceiver.
func newInterruptReceiver(notifier signalNotifier, cmds ...RecipeCommand) receiver {
	return &interruptReceiver{notifier: notifier, cmds: cmds}
}

// start listens for OS interrupts and pushes commands onto out.
// Each interrupt triggers the next command in cmds.
// After the last command is triggered, default OS interrupt behavior is restored.
func (r *interruptReceiver) start(out chan<- RecipeCommand) {
	if len(r.cmds) == 0 {
		return
	}

	sigCh := make(chan os.Signal, 1)
	r.notifier.Notify(sigCh, os.Interrupt)

	go func() {
		defer func() {
			r.notifier.Reset(os.Interrupt)
			r.notifier.Stop(sigCh)

			// Reraise interrupt if there's one pending
			select {
			case <-sigCh:
				_ = r.notifier.Signal(os.Interrupt)
			default:
			}
		}()

		interruptLevel := 0
		for {
			<-sigCh
			out <- r.cmds[interruptLevel]
			interruptLevel++

			if interruptLevel >= len(r.cmds) {
				return
			}
		}
	}()
}
