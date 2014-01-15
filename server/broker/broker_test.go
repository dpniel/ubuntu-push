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

package broker

import (
	"fmt"
	. "launchpad.net/gocheck"
	"testing"
)

func TestBroker(t *testing.T) { TestingT(t) }

type brokerSuite struct{}

var _ = Suite(&brokerSuite{})

func (s *brokerSuite) TestErrAbort(c *C) {
	err := &ErrAbort{"expected FOO"}
	c.Check(fmt.Sprintf("%s", err), Equals, "session aborted (expected FOO)")
}