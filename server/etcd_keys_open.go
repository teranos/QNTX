//go:build cgo && rustetcd

package server

import (
	"os"
	"strings"

	"github.com/teranos/QNTX/ats/storage/etcdcgo"
	appcfg "github.com/teranos/QNTX/internal/config"
	errors "github.com/teranos/sacred-error"
)

// "names and collections and just smaller things, messages say's status"
//
// openEtcdKeys opens the keys under prefix as the user am.toml's [etcd] names,
// with the password its password_file holds; as nobody when it names none.
func openEtcdKeys(endpoint, prefix string) (*etcdcgo.Keys, error) {
	cfg, err := appcfg.Load()
	if err != nil {
		return nil, errors.Wrap(err, "am.toml was not read for the etcd user")
	}
	user, password := cfg.Etcd.User, ""
	if user != "" {
		if cfg.Etcd.PasswordFile == "" {
			return nil, errors.Newf("am.toml's [etcd] names the user %q and no password_file", user)
		}
		held, err := os.ReadFile(cfg.Etcd.PasswordFile)
		if err != nil {
			return nil, errors.Wrapf(err, "the password of the etcd user %q was not read", user)
		}
		password = strings.TrimSpace(string(held))
	}
	return etcdcgo.OpenKeysAs(endpoint, prefix, user, password)
}
