package claudecode

import (
	"debug/elf"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	errors "github.com/teranos/sacred-error"
)

// Claude Code is fetched as built for a generic Linux, naming /lib64/ld-linux-x86-64.so.2.
// NixOS has none there; patched to another loader, its embedded payload faults.

// Invocation runs binary as fetched where the loader it names is there, and
// through the loader this node runs on, handed binary, where it is not.
func Invocation(binary string, args []string) (string, []string, error) {
	if runtime.GOOS != "linux" {
		return binary, args, nil
	}
	wants, isELF, err := interpreterOf(binary)
	if err != nil {
		return "", []string{}, err
	}
	// A script, as a stand-in for Claude Code is, names no loader.
	if !isELF {
		return binary, args, nil
	}
	_, statErr := os.Stat(wants)
	if statErr == nil {
		// NixOS keeps a stub at the generic loader's path that only says it cannot run the ELF.
		loads, err := isLoader(wants)
		if err != nil {
			return "", []string{}, err
		}
		if loads {
			return binary, args, nil
		}
	} else if !os.IsNotExist(statErr) {
		return "", []string{}, errors.Wrapf(statErr, "whether %s, the loader %s names, is on this machine did not read", wants, binary)
	}
	self, err := os.Executable()
	if err != nil {
		return "", []string{}, errors.Wrap(err, "where this node's own binary is did not read")
	}
	own, selfELF, err := interpreterOf(self)
	if err != nil {
		return "", []string{}, err
	}
	if !selfELF || own == wants {
		return "", []string{}, errors.Newf("%s names %s as its loader, which this machine does not have, and this node runs on no other to run it with", binary, wants)
	}
	return own, append([]string{"--library-path", filepath.Dir(own), binary}, args...), nil
}

// isLoader is whether the file at path is a loader: an ELF shared object naming
// itself, as ld-linux does. NixOS's stub-ld is an executable and names nothing.
func isLoader(path string) (loads bool, err error) {
	file, err := elf.Open(path)
	var notELF *elf.FormatError
	if errors.As(err, &notELF) {
		return false, nil
	}
	if err != nil {
		return false, errors.Wrapf(err, "%s did not open to read whether it is a loader", path)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.WithSecondaryError(err, errors.Wrapf(closeErr, "%s did not close", path))
		}
	}()
	if file.Type != elf.ET_DYN {
		return false, nil
	}
	names, err := file.DynString(elf.DT_SONAME)
	if err != nil {
		return false, errors.Wrapf(err, "the name %s gives itself did not read", path)
	}
	return len(names) > 0, nil
}

// interpreterOf is the loader the ELF at path names, and whether path is an ELF at all.
func interpreterOf(path string) (loader string, isELF bool, err error) {
	file, err := elf.Open(path)
	var notELF *elf.FormatError
	if errors.As(err, &notELF) {
		return "", false, nil
	}
	if err != nil {
		return "", false, errors.Wrapf(err, "%s did not open to read its loader", path)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.WithSecondaryError(err, errors.Wrapf(closeErr, "%s did not close", path))
		}
	}()
	for _, prog := range file.Progs {
		if prog.Type != elf.PT_INTERP {
			continue
		}
		named := make([]byte, prog.Filesz)
		read, err := prog.ReadAt(named, 0)
		if err != nil {
			return "", true, errors.Wrapf(err, "the loader %s names did not read, %d bytes of %d", path, read, prog.Filesz)
		}
		return strings.TrimRight(string(named), "\x00"), true, nil
	}
	return "", true, errors.Newf("%s names no loader", path)
}
