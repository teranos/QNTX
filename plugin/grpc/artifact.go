package grpc

// A plugin built by a runner on another machine reaches the node as a GitHub
// Actions artifact of its workflow run (ADR-043): the zip GitHub serves,
// holding the archive the package step wrote and its .sha256.
//
// The artifact is read where it lies, on disk, and never held whole: q, with
// 1,936 MB of memory, held otaripy's 526 MB artifact once as the zip and again
// as the archive in it.

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"

	"github.com/teranos/QNTX/internal/sqlclose"
	errors "github.com/teranos/sacred-error"
)

// ArtifactArchive is a plugin's build inside an artifact. It is read out of
// the artifact each time it is opened.
type ArtifactArchive struct{ file *zip.File }

// Name is the archive's name in the artifact.
func (a ArtifactArchive) Name() string { return a.file.Name }

// Open reads the archive out of the artifact. The zip's own check of what it
// holds is said when the reading reaches its end.
func (a ArtifactArchive) Open() (io.ReadCloser, error) {
	opened, err := a.file.Open()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open %s in the artifact", a.file.Name)
	}
	return opened, nil
}

// ArtifactBuild is name's build for this platform out of an artifact of size
// bytes, checked against the .sha256 beside it as it is read, and its digest.
func ArtifactBuild(name string, zipped io.ReaderAt, size int64) (ArtifactArchive, string, error) {
	read, err := zip.NewReader(zipped, size)
	if err != nil {
		return ArtifactArchive{}, "", errors.Wrapf(err, "the artifact of %s is not a zip", name)
	}
	held := map[string]*zip.File{}
	for _, file := range read.File {
		held[file.Name] = file
	}
	for _, file := range read.File {
		if plugin, ok := BuildPlugin(file.Name); !ok || plugin != name {
			continue
		}
		digestName := file.Name + digestSuffix
		digestFile, ok := held[digestName]
		if !ok {
			return ArtifactArchive{}, "", errors.Newf("the artifact holds %s and no %s beside it; nothing was installed", file.Name, digestName)
		}
		archive := ArtifactArchive{file: file}
		got, err := digestOf(archive)
		if err != nil {
			return ArtifactArchive{}, "", err
		}
		said, err := unzipped(digestFile)
		if err != nil {
			return ArtifactArchive{}, "", err
		}
		digest, err := checkedSum(file.Name, got, digestName, said)
		if err != nil {
			return ArtifactArchive{}, "", err
		}
		return archive, digest, nil
	}
	return ArtifactArchive{}, "", errors.Newf("the artifact holds no build of %s for %s", name, strings.TrimPrefix(pluginAssetSuffix(), "-"))
}

// digestOf is the sha256 of what archive holds, read through once.
func digestOf(archive ArtifactArchive) (_ string, err error) {
	opened, err := archive.Open()
	if err != nil {
		return "", err
	}
	defer func() { err = sqlclose.With(err, opened.Close(), archive.Name()+" in the artifact") }()
	sum := sha256.New()
	if read, err := io.Copy(sum, opened); err != nil {
		return "", errors.Wrapf(err, "failed to read %s out of the artifact after %d bytes", archive.Name(), read)
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

// unzipped is what one small file of an artifact holds: its .sha256.
func unzipped(file *zip.File) ([]byte, error) {
	opened, err := file.Open()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open %s in the artifact", file.Name)
	}
	data, err := io.ReadAll(opened)
	if err != nil {
		err = errors.Wrapf(err, "failed to read %s out of the artifact after %d bytes", file.Name, len(data))
	}
	return data, sqlclose.With(err, opened.Close(), file.Name+" in the artifact")
}
