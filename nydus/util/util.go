package util

import (
	"archive/tar"
	"context"
	"fmt"
	"io"

	containerdcompression "github.com/containerd/containerd/archive/compression"
	"github.com/opencontainers/go-digest"
)

type fsVersionKey struct{}
type compressorKey struct{}
type chunkDictDigestKey struct{}

func WithContext(ctx context.Context, fsVersion string, compressor string, chunkDictDigest digest.Digest) context.Context {
	if fsVersion == "" {
		fsVersion = "5"
	}

	ctx = context.WithValue(ctx, fsVersionKey{}, fsVersion)
	ctx = context.WithValue(ctx, compressorKey{}, compressor)
	if chunkDictDigest != "" {
		ctx = context.WithValue(ctx, chunkDictDigestKey{}, chunkDictDigest.String())
	}

	return ctx
}

func GetContext(ctx context.Context) (string, string, string) {
	fsVersion := ""
	compressor := ""
	chunkDictDigest := ""

	ctxValue := ctx.Value(fsVersionKey{})
	if ctxValue != nil {
		fsVersion = ctxValue.(string)
	}

	ctxValue = ctx.Value(chunkDictDigestKey{})
	if ctxValue != nil {
		chunkDictDigest = ctxValue.(string)
	}

	ctxValue = ctx.Value(compressorKey{})
	if ctxValue != nil {
		compressor = ctxValue.(string)
	}

	return fsVersion, compressor, chunkDictDigest
}

func UnpackFile(reader io.Reader, source string, target io.Writer) error {
	rdr, err := containerdcompression.DecompressStream(reader)
	if err != nil {
		return err
	}
	defer rdr.Close()

	found := false
	tr := tar.NewReader(rdr)
	for {
		hdr, err := tr.Next()
		if err != nil {
			if err == io.EOF {
				break
			} else {
				return err
			}
		}
		if hdr.Name == source {
			if _, err := io.Copy(target, tr); err != nil {
				return err
			}
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("not found file %s in targz", source)
	}

	return nil
}
