// Copyright 2026 Aeneas Rekkas
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
)

func newStdioRunContext() (context.Context, func(), func() string) {
	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, stdioShutdownSignals()...)

	var mu sync.RWMutex
	var reason string

	setReason := func(sig os.Signal) {
		mu.Lock()
		reason = fmt.Sprintf("signal: %s", sig)
		mu.Unlock()
	}
	getReason := func() string {
		mu.RLock()
		defer mu.RUnlock()
		return reason
	}

	go func() {
		select {
		case sig := <-sigCh:
			setReason(sig)
			signal.Stop(sigCh)
			cancel()
		case <-ctx.Done():
		}
	}()

	stop := func() {
		signal.Stop(sigCh)
		cancel()
	}
	return ctx, stop, getReason
}
