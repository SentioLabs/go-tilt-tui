package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/sentiolabs/selfupdate-go"
)

// The help texts follow selfupdate-go's cobracmd, which envctl uses.
const (
	selfUsage = `Usage: tilt-tui self <command>

Manage the tilt-tui CLI itself.

Commands:
  update   Update tilt-tui to the latest version
  channel  View or switch the update channel

Run 'tilt-tui self <command> -h' for a command's flags.
`

	updateUsage = `Usage: tilt-tui self update [--check] [-f|--force] [-y|--yes]

Update tilt-tui to the latest release on the current update channel.

Use 'tilt-tui self channel' to view or switch the channel.

Examples:
  tilt-tui self update          Update if a new version is available
  tilt-tui self update --check  Check for updates without installing
  tilt-tui self update --force  Reinstall even if already up to date
  tilt-tui self update -y       Update without a confirmation prompt

Flags:
`

	channelUsage = `Usage: tilt-tui self channel [stable|rc|nightly] [-y|--yes]

View or switch the update channel used by 'tilt-tui self update'.

Channels:
  stable   Official releases (default)
  rc       Release candidates
  nightly  Daily builds from the main branch

Examples:
  tilt-tui self channel            Show the current channel
  tilt-tui self channel rc         Switch to rc (asks for confirmation)
  tilt-tui self channel rc -y      Switch without prompting

Flags:
`
)

// runSelf runs `tilt-tui self ...`. args excludes "self". Usage and flag
// errors go to out. A help request prints usage and returns nil.
func runSelf(ctx context.Context, u *selfupdate.Updater, args []string, out io.Writer) error {
	if len(args) == 0 {
		_, err := fmt.Fprint(out, selfUsage)
		return err
	}
	var err error
	switch args[0] {
	case "-h", "-help", "--help", "help":
		_, err = fmt.Fprint(out, selfUsage)
	case "update":
		err = selfUpdate(ctx, u, args[1:], out)
	case "channel":
		err = selfChannel(u, args[1:], out)
	default:
		_, _ = fmt.Fprint(out, selfUsage)
		return fmt.Errorf("unknown self command %q", args[0])
	}
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	return err
}

// selfFlagSet returns a flag set named for the subcommand whose -h
// prints usage and then the flag defaults.
func selfFlagSet(name, usage string, out io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("tilt-tui self "+name, flag.ContinueOnError)
	fs.SetOutput(out)
	fs.Usage = func() {
		_, _ = fmt.Fprint(fs.Output(), usage)
		fs.PrintDefaults()
	}
	return fs
}

// selfUpdate runs `self update [--check] [-f|--force] [-y|--yes]`.
func selfUpdate(ctx context.Context, u *selfupdate.Updater, args []string, out io.Writer) error {
	var opts selfupdate.UpdateOptions
	fs := selfFlagSet("update", updateUsage, out)
	fs.BoolVar(&opts.Check, "check", false, "check for updates without installing")
	fs.BoolVar(&opts.Force, "force", false, "reinstall even if already up to date")
	fs.BoolVar(&opts.Force, "f", false, "short for --force")
	fs.BoolVar(&opts.Yes, "yes", false, "skip the confirmation prompt")
	fs.BoolVar(&opts.Yes, "y", false, "short for --yes")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q: self update takes no arguments", fs.Arg(0))
	}
	return u.Update(ctx, opts)
}

// selfChannel runs `self channel [stable|rc|nightly] [-y|--yes]`. The
// flags may come before or after the channel name.
func selfChannel(u *selfupdate.Updater, args []string, out io.Writer) error {
	var yes bool
	fs := selfFlagSet("channel", channelUsage, out)
	fs.BoolVar(&yes, "yes", false, "skip the confirmation prompt")
	fs.BoolVar(&yes, "y", false, "short for --yes")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		ch, err := u.CurrentChannel()
		if err != nil {
			return err
		}
		w := u.Out
		if w == nil {
			w = out
		}
		_, err = fmt.Fprintf(w, "Current update channel: %s\n", ch)
		return err
	}
	name := fs.Arg(0)
	if err := fs.Parse(fs.Args()[1:]); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q: self channel takes at most one channel", fs.Arg(0))
	}
	return u.SwitchChannel(selfupdate.Channel(name), yes)
}
