// +build linux

package pixelproxy

import (
	"context"
	"os/exec"
	"syscall"

	"github.com/danjacques/pixelproxy/util"
	"github.com/danjacques/pixelproxy/util/logging"

	"github.com/pkg/errors"
)

func getSudoShutdownCommands() (sudo, shutdown string, err error) {
	if sudo, err = exec.LookPath("sudo"); err != nil {
		err = errors.Wrap(err, "could not find 'sudo'")
		return
	}

	if shutdown, err = exec.LookPath("shutdown"); err != nil {
		err = errors.Wrap(err, "could not find 'shutdown'")
		return
	}

	return
}

// DefaultSystemControl implements SystemControl, returning an error for each
// command.
var DefaultSystemControl = &SystemControl{
	ValidateAccess: func(c context.Context) error {
		sudo, shutdown, err := getSudoShutdownCommands()
		if err != nil {
			return err
		}

		err = runCommand(c, false, sudo, "--non-interactive", "--list", "--", shutdown)
		if err != nil {
			return errors.Wrap(err, "user does not have permission")
		}
		return nil
	},

	Shutdown: func(c context.Context) error {
		sudo, shutdown, err := getSudoShutdownCommands()
		if err != nil {
			return err
		}
		return runCommand(c, true, sudo, "--non-interactive", "--", shutdown, "--poweroff", "now")
	},

	Restart: func(c context.Context) error {
		sudo, shutdown, err := getSudoShutdownCommands()
		if err != nil {
			return err
		}

		err = runCommand(c, true, sudo, "--non-interactive", "--", shutdown, "--reboot", "now")
		if err != nil {
			return errors.Wrap(err, "user does not have permission")
		}
		return nil
	},
}

func runCommand(c context.Context, realCommand bool, name string, args ...string) error {
	logInfo, logError := logging.S(c).Infof, logging.S(c).Errorf
	if !realCommand {
		// If this is a probe command, don't log significance.
		logError, logInfo = logInfo, logging.S(c).Debugf
	}

	argSlice := &util.StringSlice{S: args, Delim: " "}
	logInfo("Running system command: %s %s", name, argSlice)

	cmd := exec.CommandContext(c, name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if exiterr, ok := err.(*exec.ExitError); ok {
			if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
				logError("Command (%s %s) failed with exit code %d and output:\n%s",
					name, argSlice, status.ExitStatus(), output)
				return err
			}
		}

		logError("Command (%s %s) failed to execute, output:\n%s", name, argSlice, output)
		return err
	}

	logInfo("Command (%s %s) finished successfully with output:\n%s", name, argSlice, output)
	return nil
}
