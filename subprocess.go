/*******************************************************************************
*
* Copyright 2022 SAP SE
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You should have received a copy of the License along with this
* program. If not, you may obtain a copy of the License at
*
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
*
*******************************************************************************/

package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"time"
)

func buildKubectlPortForwardCmdline(args []string) []string {
	return append([]string{"kubectl", "port-forward"}, args...)
}

func buildCloudSqlProxyCmdline(args []string) []string {
	return append([]string{"cloud-sql-proxy"}, args...)
}

func runKubectlPortForward(ctx context.Context, args []string, errChan chan<- error, portReadableChan chan<- struct{}) {
	cmdline := buildKubectlPortForwardCmdline(args)
	stderr := io.MultiWriter(&portReadableDetector{portReadableChan: portReadableChan}, os.Stderr)
	cmd := exec.CommandContext(ctx, cmdline[0], cmdline[1:]...) //nolint:gosec // we explicitly want to pass through the user-supplied command
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 3 * time.Second
	cmd.Stdin = nil
	cmd.Stdout = stderr // os.Stdout is exclusively reserved for the actual payload command
	cmd.Stderr = stderr
	errChan <- cmd.Run()
}

func runCloudSqlProxy(ctx context.Context, args []string, errChan chan<- error, proxyStartedChan chan<- struct{}) {
	cmdline := buildCloudSqlProxyCmdline(args)
	stderr := io.MultiWriter(&proxyStartedDetector{proxyStartedChan: proxyStartedChan}, os.Stderr)
	cmd := exec.CommandContext(ctx, cmdline[0], cmdline[1:]...) //nolint:gosec // we explicitly want to pass through the user-supplied command
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 3 * time.Second
	cmd.Stdin = nil
	cmd.Stdout = stderr // os.Stdout is exclusively reserved for the actual payload command
	cmd.Stderr = stderr
	errChan <- cmd.Run()
}

// portReadableDetector is an io.Writer that looks for the info message from `kubectl port-forward` about port bindings being established.
type portReadableDetector struct {
	done             bool
	portReadableChan chan<- struct{}
}

// Write implements the io.Writer interface.
func (d *portReadableDetector) Write(buf []byte) (int, error) {
	if !d.done && bytes.Contains(buf, []byte("Forwarding from")) {
		go func() {
			// give kubectl some extra time if it needs to listen on multiple ports
			time.Sleep(25 * time.Microsecond)
			close(d.portReadableChan)
		}()
		d.done = true
	}
	return len(buf), nil
}

// proxyStartedDetector is an io.Writer that looks for the info message from `cloud_sql_proxy` about the proxy having started.
type proxyStartedDetector struct {
	done             bool
	proxyStartedChan chan<- struct{}
}

// Write implements the io.Writer interface.
func (d *proxyStartedDetector) Write(buf []byte) (int, error) {
	if !d.done && bytes.Contains(buf, []byte("The proxy has started successfully")) {
		go func() {
			// give cloud_sql_proxy some extra time if it needs to listen on multiple ports
			time.Sleep(25 * time.Microsecond)
			close(d.proxyStartedChan)
		}()
		d.done = true
	}
	return len(buf), nil
}

func runSubcommand(ctx context.Context, cmdline []string, errChan chan<- error, readyChan <-chan struct{}) {
	// wait for either the port-forward to become active, or for the port-forward
	// failing and its failure being signaled to us by canceling `ctx`
	select {
	case <-readyChan:
		// continue below
	case <-ctx.Done():
		return
	}

	cmd := exec.CommandContext(ctx, cmdline[0], cmdline[1:]...) //nolint:gosec // we explicitly want to pass through the user-supplied command
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 3 * time.Second
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	errChan <- cmd.Run()
}
