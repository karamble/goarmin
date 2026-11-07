// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package bluez

import (
	"context"
	"fmt"
	"sync"

	"github.com/godbus/dbus/v5"
)

// Prompter answers the questions bluetoothd asks during pairing. Implementations decide how
// to involve the user: a terminal, a web UI, or a fixed answer in tests.
type Prompter interface {
	// RequestPasskey asks for the 6-digit passkey the remote device displays.
	RequestPasskey(dev Device) (uint32, error)
	// RequestConfirmation asks whether both sides show the same passkey (numeric comparison).
	RequestConfirmation(dev Device, passkey uint32) (bool, error)
	// DisplayPasskey shows a passkey the user has to enter on the remote device.
	DisplayPasskey(dev Device, passkey uint32, entered uint16)
	// Cancel reports that bluetoothd aborted the pending request.
	Cancel()
}

// Agent is an org.bluez.Agent1 that only answers for one device, the one being paired, and
// rejects requests for any other device.
type Agent struct {
	c        *Client
	path     dbus.ObjectPath
	prompter Prompter

	mu     sync.Mutex
	target Device
}

const agentPath = "/goarmin/agent"

// agentErr returns a BlueZ agent error. Rejected and Canceled are the names bluetoothd expects.
func agentErr(name string) *dbus.Error {
	return dbus.NewError("org.bluez.Error."+name, nil)
}

// AgentOptions tunes RegisterAgent.
type AgentOptions struct {
	// Default also makes the agent the system's default agent, which answers pairing requests
	// the remote side starts. Pairing we start (Device1.Pair on this client) is answered by our
	// agent either way, because bluetoothd asks the agent of whoever called Pair. Leave it off,
	// so the desktop's own agent keeps answering for every other device.
	Default bool
}

// RegisterAgent exports an agent with capability KeyboardDisplay, restricted to target. With
// opt.Default it also becomes the default agent. The returned function unregisters it.
func (c *Client) RegisterAgent(ctx context.Context, target Device, p Prompter, opt AgentOptions) (*Agent, func(), error) {
	a := &Agent{c: c, path: agentPath, prompter: p, target: target}
	if err := c.conn.Export(a, a.path, ifaceAgent); err != nil {
		return nil, nil, fmt.Errorf("bluez: export agent: %w", err)
	}
	mgr := dbus.ObjectPath(rootPath)
	if err := c.call(ctx, mgr, ifaceAgentMgr+".RegisterAgent", a.path, "KeyboardDisplay").Err; err != nil {
		_ = c.conn.Export(nil, a.path, ifaceAgent)
		return nil, nil, fmt.Errorf("bluez: RegisterAgent: %w", err)
	}
	if opt.Default {
		if err := c.call(ctx, mgr, ifaceAgentMgr+".RequestDefaultAgent", a.path).Err; err != nil {
			_ = c.call(ctx, mgr, ifaceAgentMgr+".UnregisterAgent", a.path).Err
			_ = c.conn.Export(nil, a.path, ifaceAgent)
			return nil, nil, fmt.Errorf("bluez: RequestDefaultAgent: %w", err)
		}
	}
	unregister := func() {
		_ = c.conn.Object(service, mgr).Call(ifaceAgentMgr+".UnregisterAgent", 0, a.path).Err
		_ = c.conn.Export(nil, a.path, ifaceAgent)
	}
	return a, unregister, nil
}

// allowed reports whether a request concerns the target device.
func (a *Agent) allowed(path dbus.ObjectPath) (Device, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.target, path == a.target.Path
}

// The methods below implement org.bluez.Agent1; godbus calls them from its own goroutine.

func (a *Agent) Release() *dbus.Error { return nil }

func (a *Agent) RequestPinCode(device dbus.ObjectPath) (string, *dbus.Error) {
	return "", agentErr("Rejected") // legacy PIN pairing is not used by LE watches
}

func (a *Agent) DisplayPinCode(device dbus.ObjectPath, pincode string) *dbus.Error {
	return agentErr("Rejected")
}

func (a *Agent) RequestPasskey(device dbus.ObjectPath) (uint32, *dbus.Error) {
	d, ok := a.allowed(device)
	if !ok {
		return 0, agentErr("Rejected")
	}
	pk, err := a.prompter.RequestPasskey(d)
	if err != nil {
		return 0, agentErr("Canceled")
	}
	return pk, nil
}

func (a *Agent) DisplayPasskey(device dbus.ObjectPath, passkey uint32, entered uint16) *dbus.Error {
	if d, ok := a.allowed(device); ok {
		a.prompter.DisplayPasskey(d, passkey, entered)
	}
	return nil
}

func (a *Agent) RequestConfirmation(device dbus.ObjectPath, passkey uint32) *dbus.Error {
	d, ok := a.allowed(device)
	if !ok {
		return agentErr("Rejected")
	}
	yes, err := a.prompter.RequestConfirmation(d, passkey)
	if err != nil {
		return agentErr("Canceled")
	}
	if !yes {
		return agentErr("Rejected")
	}
	return nil
}

func (a *Agent) RequestAuthorization(device dbus.ObjectPath) *dbus.Error {
	if _, ok := a.allowed(device); !ok {
		return agentErr("Rejected")
	}
	return nil
}

func (a *Agent) AuthorizeService(device dbus.ObjectPath, uuid string) *dbus.Error {
	if _, ok := a.allowed(device); !ok {
		return agentErr("Rejected")
	}
	return nil
}

func (a *Agent) Cancel() *dbus.Error {
	a.prompter.Cancel()
	return nil
}
