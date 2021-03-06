/*
 Copyright 2013-2014 Canonical Ltd.

 This program is free software: you can redistribute it and/or modify it
 under the terms of the GNU General Public License version 3, as published
 by the Free Software Foundation.

 This program is distributed in the hope that it will be useful, but
 WITHOUT ANY WARRANTY; without even the implied warranties of
 MERCHANTABILITY, SATISFACTORY QUALITY, or FITNESS FOR A PARTICULAR
 PURPOSE.  See the GNU General Public License for more details.

 You should have received a copy of the GNU General Public License along
 with this program.  If not, see <http://www.gnu.org/licenses/>.
*/

package protocol

// Structs representing messages.

import (
	"encoding/json"
	"fmt"
)

// System channel id using a shortened hex-encoded form for the NIL UUID.
const SystemChannelId = "0"

// CONNECT message
type ConnectMsg struct {
	Type          string `json:"T"`
	ClientVer     string
	DeviceId      string
	Authorization string
	Cookie        string
	Info          map[string]interface{} `json:",omitempty"` // platform etc...
	// maps channel ids (hex encoded UUIDs) to known client channel levels
	Levels map[string]int64
}

// CONNACK message
type ConnAckMsg struct {
	Type   string `json:"T"`
	Params ConnAckParams
}

// ConnAckParams carries the connection parameters from the server on
// connection acknowledgement.
type ConnAckParams struct {
	// ping interval formatted time.Duration
	PingInterval string
}

// SplittableMsg are messages that may require and are capable of splitting.
type SplittableMsg interface {
	Split() (done bool)
}

// OnewayMsg are messages that are not to be followed by a response,
// after sending them the session either aborts or continues.
type OnewayMsg interface {
	SplittableMsg
	// continue session after the message?
	OnewayContinue() bool
}

// CONNBROKEN message, server side is breaking the connection for reason.
type ConnBrokenMsg struct {
	Type string `json:"T"`
	// reason
	Reason string
}

func (m *ConnBrokenMsg) Split() bool {
	return true
}

func (m *ConnBrokenMsg) OnewayContinue() bool {
	return false
}

// CONNBROKEN reasons
const (
	BrokenHostMismatch = "host-mismatch"
)

// CONNWARN message, server side is warning about partial functionality
// because reason.
type ConnWarnMsg struct {
	Type string `json:"T"`
	// reason
	Reason string
}

func (m *ConnWarnMsg) Split() bool {
	return true
}
func (m *ConnWarnMsg) OnewayContinue() bool {
	return true
}

// CONNWARN reasons
const (
	WarnUnauthorized = "unauthorized"
)

// SETPARAMS message
type SetParamsMsg struct {
	Type      string `json:"T"`
	SetCookie string
}

func (m *SetParamsMsg) Split() bool {
	return true
}
func (m *SetParamsMsg) OnewayContinue() bool {
	return true
}

// PING/PONG messages
type PingPongMsg struct {
	Type string `json:"T"`
}

const maxPayloadSize = 62 * 1024

// BROADCAST messages
type BroadcastMsg struct {
	Type      string `json:"T"`
	AppId     string `json:",omitempty"`
	ChanId    string
	TopLevel  int64
	Payloads  []json.RawMessage
	splitting int
}

func (m *BroadcastMsg) Split() bool {
	var prevTop int64
	if m.splitting == 0 {
		prevTop = m.TopLevel - int64(len(m.Payloads))
	} else {
		prevTop = m.TopLevel
		m.Payloads = m.Payloads[len(m.Payloads):m.splitting]
		m.TopLevel = prevTop + int64(len(m.Payloads))
	}
	payloads := m.Payloads
	var size int
	for i := range payloads {
		size += len(payloads[i])
		if size > maxPayloadSize {
			m.TopLevel = prevTop + int64(i)
			m.splitting = len(payloads)
			m.Payloads = payloads[:i]
			return false
		}
	}
	return true
}

// Reset resets the splitting state if the message storage is to be
// reused and sets the proper Type.
func (b *BroadcastMsg) Reset() {
	b.Type = "broadcast"
	b.splitting = 0
}

// NOTIFICATIONS message
type NotificationsMsg struct {
	Type          string `json:"T"`
	Notifications []Notification
	splitting     int
}

// Reset resets the splitting state if the message storage is to be
// reused and sets the proper Type.
func (m *NotificationsMsg) Reset() {
	m.Type = "notifications"
	m.splitting = 0
}

func (m *NotificationsMsg) Split() bool {
	if m.splitting != 0 {
		m.Notifications = m.Notifications[len(m.Notifications):m.splitting]
	}
	notifs := m.Notifications
	var size int
	for i, notif := range notifs {
		size += len(notif.Payload) + len(notif.AppId) + len(notif.MsgId) + notificationOverhead
		if size > maxPayloadSize {
			m.splitting = len(notifs)
			m.Notifications = notifs[:i]
			return false
		}
	}
	return true
}

var notificationOverhead int

func init() {
	buf, err := json.Marshal(Notification{})
	if err != nil {
		panic(fmt.Errorf("failed to compute Notification marshal overhead: %v", err))
	}
	notificationOverhead = len(buf) - 4 // - 4 for the null from P(ayload)
}

// A single unicast notification
type Notification struct {
	AppId string `json:"A"`
	MsgId string `json:"M"`
	// payload
	Payload json.RawMessage `json:"P"`
}

// ExtractPayloads gets only the payloads out of a slice of notications.
func ExtractPayloads(notifications []Notification) []json.RawMessage {
	n := len(notifications)
	if n == 0 {
		return nil
	}
	payloads := make([]json.RawMessage, n)
	for i := 0; i < n; i++ {
		payloads[i] = notifications[i].Payload
	}
	return payloads
}

// ACKnowledgement message
type AckMsg struct {
	Type string `json:"T"`
}

// xxx ... query levels messages
