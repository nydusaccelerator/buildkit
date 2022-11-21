package util

import (
	"archive/tar"
	"context"
	"fmt"
	"io"

	containerdcompression "github.com/containerd/containerd/archive/compression"
	"github.com/opencontainers/go-digest"
)

type blobLinkKey struct{}

const NydusBlobLinkPrefix = "nydus-blob-link"

func WithNydusBlobLinkKey(ctx context.Context, fsVersion string, chunkDictDigest digest.Digest) context.Context {
	hasKey := false
	if fsVersion != "" {
		fsVersion = fmt.Sprintf("-v%s", fsVersion)
		hasKey = true
	}
	chunkDictDigestStr := "-chunk-dict-none"
	if chunkDictDigest != "" {
		chunkDictDigestStr = fmt.Sprintf("-chunk-dict-%s", chunkDictDigest.String())
		hasKey = true
	}
	if hasKey {
		blobLinkKeyStr := fmt.Sprintf("%s%s%s-", NydusBlobLinkPrefix, fsVersion, chunkDictDigestStr)
		return context.WithValue(ctx, blobLinkKey{}, blobLinkKeyStr)
	}
	return ctx
}

func GetNydusBlobLinkKey(ctx context.Context) string {
	nydusBlobLinkKey := ""
	ctxValue := ctx.Value(blobLinkKey{})
	if ctxValue != nil {
		nydusBlobLinkKey = ctxValue.(string)
	}
	return nydusBlobLinkKey
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
