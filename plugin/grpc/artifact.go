package grpc

// A plugin built by a runner on another machine reaches the node as a GitHub
// Actions artifact of its workflow run (ADR-043): the zip GitHub serves,
// holding the archive the package step wrote and its .sha256.

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"

	"github.com/teranos/QNTX/internal/sqlclose"
	errors "github.com/teranos/sacred-error"
)

// ArtifactBuild is name's build for this platform out of an artifact, checked
// against the .sha256 beside it, and its digest.
func ArtifactBuild(name string, zipped []byte) ([]byte, string, error) {
	read, err := zip.NewReader(bytes.NewReader(zipped), int64(len(zipped)))
	if err != nil {
		return nil, "", errors.Wrapf(err, "the artifact of %s is not a zip", name)
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
			return nil, "", errors.Newf("the artifact holds %s and no %s beside it; nothing was installed", file.Name, digestName)
		}
		archive, err := unzipped(file)
		if err != nil {
			return nil, "", err
		}
		said, err := unzipped(digestFile)
		if err != nil {
			return nil, "", err
		}
		digest, err := checkedDigest(file.Name, archive, digestName, said)
		if err != nil {
			return nil, "", err
		}
		return archive, digest, nil
	}
	return nil, "", errors.Newf("the artifact holds no build of %s for %s", name, strings.TrimPrefix(pluginAssetSuffix(), "-"))
}

// unzipped is what one file of an artifact holds.
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
