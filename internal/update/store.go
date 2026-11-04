package update

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/sentiolabs/selfupdate-go"
)

// channelFile is the TOML shape of selfupdate.toml.
type channelFile struct {
	Channel string `toml:"channel"`
}

// ChannelStore keeps the channel in dir/selfupdate.toml. A missing file
// reads as "", which the updater treats as stable. It never writes
// config.toml.
func ChannelStore(dir string) selfupdate.Store {
	path := filepath.Join(dir, ChannelFile)
	return selfupdate.FuncStore{
		Get: func() (selfupdate.Channel, error) { return readChannel(path) },
		Set: func(c selfupdate.Channel) error { return writeChannel(dir, path, c) },
	}
}

// readChannel decodes the channel from path. A missing file is "".
func readChannel(path string) (selfupdate.Channel, error) {
	var f channelFile
	if _, err := toml.DecodeFile(path, &f); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return selfupdate.Channel(f.Channel), nil
}

// writeChannel creates dir with mode 0700 when missing, writes c to a
// temp file in dir with mode 0600 and renames it over path.
func writeChannel(dir, path string, c selfupdate.Channel) (err error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	tmp, err := os.CreateTemp(dir, ".selfupdate-*.toml")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if err := toml.NewEncoder(tmp).Encode(channelFile{Channel: string(c)}); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
