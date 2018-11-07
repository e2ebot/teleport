/*
Copyright 2016 Gravitational, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package srv

import (
	"context"
	"fmt"
	"time"

	"github.com/gravitational/teleport"
	"github.com/gravitational/teleport/lib/defaults"

	"github.com/sirupsen/logrus"
)

// RequestSender is an interface that impliments SendRequest. It is used so
// server and client connections can be passed to functions to send requests.
type RequestSender interface {
	// SendRequest is used to send a out-of-band request.
	SendRequest(name string, wantReply bool, payload []byte) (bool, []byte, error)
}

type KeepAliveConfig struct {
	Conns []RequestSender

	Timeout time.Duration

	CloseContext context.Context
	CloseCancel  context.CancelFunc
}

func StartKeepAliveLoop(config KeepAliveConfig) {
	var missedCount int

	fmt.Printf("--> config.Timeout: %v\n", config.Timeout)

	//// If no timeout was requested, don't even start the keep-alive loop.
	//if config.Timeout == 0 {
	//	log.Debugf("Keep-alive interval of 0s was selected, closing keep-alive loop.")
	//	return
	//}

	tickerCh := time.NewTicker(config.Timeout)
	defer tickerCh.Stop()

	for {
		select {
		case <-tickerCh.C:
			var sentCount int

			// Send a keep alive message on all connections and make sure a response
			// was received on all.
			for _, conn := range config.Conns {
				ok := sendKeepAliveWithTimeout(conn, defaults.ReadHeadersTimeout)
				if ok {
					sentCount = sentCount + 1
				}
			}
			if sentCount == len(config.Conns) {
				missedCount = 0
				continue
			}

			// If 3 keep alives are missed, the connection is dead, call cancel
			// and cleanup.
			missedCount = missedCount + 1
			if missedCount == 3 {
				logrus.Infof("Missed %v keep alive messages, closing connection.", missedCount)
				config.CloseCancel()
				return
			}
		// If an external caller closed the context (connection is done) then no
		// more need to wait around for keep alives.
		case <-config.CloseContext.Done():
			logrus.Debugf("Closing keep-alive loop.")
			return
		}
	}
}

// sendKeepAliveWithTimeout sends a keepalive@openssh.com message to the remote
// client. A manual timeout is needed here because SendRequest will wait for a
// response forever.
func sendKeepAliveWithTimeout(conn RequestSender, timeout time.Duration) bool {
	errorCh := make(chan error, 1)

	go func() {
		_, _, err := conn.SendRequest(teleport.KeepAliveReqType, true, nil)
		errorCh <- err
	}()

	select {
	case err := <-errorCh:
		if err != nil {
			return false
		}
		return true
	case <-time.After(timeout):
		return false
	}
}
