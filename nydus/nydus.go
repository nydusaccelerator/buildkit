package nydus

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"time"

	"github.com/containerd/containerd/content"
	"github.com/containerd/containerd/images"
	dockerref "github.com/containerd/containerd/reference/docker"
	"github.com/containerd/containerd/remotes/docker"
	nydusify "github.com/containerd/nydus-snapshotter/pkg/converter"
	cacheconfig "github.com/moby/buildkit/cache/config"
	nydusutil "github.com/moby/buildkit/nydus/util"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/util/compression"
	"github.com/moby/buildkit/util/flightcontrol"
	"github.com/moby/buildkit/util/progress"
	"github.com/moby/buildkit/util/resolver"
	"github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

var g flightcontrol.Group

func oneOffProgress(ctx context.Context, id string) func(err error) error {
	pw, _, _ := progress.NewFromContext(ctx)
	now := time.Now()
	st := progress.Status{
		Started: &now,
	}
	pw.Write(id, st)
	return func(err error) error {
		// TODO: set error on status
		now := time.Now()
		st.Completed = &now
		pw.Write(id, st)
		pw.Close()
		return err
	}
}

func Configure(ctx context.Context, registryHosts docker.RegistryHosts, sm *session.Manager, cs content.Store, srcRef string, refCfg *cacheconfig.RefConfig, sessionID string) context.Context {
	if refCfg.Compression.Type != compression.Nydus {
		return ctx
	}

	// Configure nydus chunk dict
	var done = func(error) error {
		return nil
	}
	chunkDictRef := refCfg.Compression.NydusChunkDictImage
	if chunkDictRef != "" {
		done = oneOffProgress(ctx, fmt.Sprintf("nydus: load chunk dict %s", chunkDictRef))
	}
	chunkDictPath, chunkDictDigest, err := loadChunkDict(ctx, registryHosts, sm, cs, srcRef, chunkDictRef, sessionID)
	done(nil)
	if chunkDictRef != "" {
		if err != nil {
			logrus.WithError(err).Warnf("nydus: load chunk dict %s", chunkDictRef)
		} else {
			refCfg.Compression.NydusChunkDictPath = chunkDictPath
		}
	}

	return nydusutil.WithNydusBlobLinkKey(ctx, refCfg.Compression.NydusFsVersion, chunkDictDigest)
}

func loadChunkDict(ctx context.Context, registryHosts docker.RegistryHosts, sm *session.Manager, cs content.Store, srcRef, chunkDictRef, sessionID string) (string, digest.Digest, error) {
	if srcRef == "" {
		return "", "", fmt.Errorf("source ref is empty")
	}

	if chunkDictRef == "" {
		return "", "", fmt.Errorf("option %s not enabled", "nydus-chunk-dict-image")
	}

	workDir := os.Getenv("NYDUS_WORKDIR")
	if workDir == "" {
		workDir = os.TempDir()
	}
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return "", "", fmt.Errorf("ensure nydus work dir")
	}

	if err := checkChunkDictRef(chunkDictRef); err != nil {
		return "", "", errors.Wrapf(err, "check chunk dict ref %s", chunkDictRef)
	}

	resolver := resolver.DefaultPool.GetResolver(registryHosts, srcRef, "pull", sm, session.NewGroup(sessionID))
	_, desc, err := resolver.Resolve(ctx, chunkDictRef)
	if err != nil {
		return "", "", errors.Wrapf(err, "resolve chunk dict ref")
	}
	if desc.MediaType != ocispecs.MediaTypeImageManifest && desc.MediaType != images.MediaTypeDockerSchema2Manifest {
		return "", "", fmt.Errorf("invalid chunk dict image media type %s", desc.MediaType)
	}

	fetcher, err := resolver.Fetcher(ctx, chunkDictRef)
	if err != nil {
		return "", "", errors.Wrapf(err, "get fetcher for chunk dict ref")
	}

	reader, err := fetcher.Fetch(ctx, desc)
	if err != nil {
		return "", "", errors.Wrapf(err, "fetch chunk dict manifest")
	}
	defer reader.Close()

	data, err := ioutil.ReadAll(reader)
	if err != nil {
		return "", "", errors.Wrapf(err, "read chunk dict manifest")
	}

	var manifest ocispecs.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return "", "", errors.Wrap(err, "unmarshal chunk dict manifest")
	}

	var bootstrapDesc *ocispecs.Descriptor
	for idx, desc := range manifest.Layers {
		if idx == len(manifest.Layers)-1 {
			if (desc.MediaType == ocispecs.MediaTypeImageLayerGzip ||
				// Found nydus bootstrap layer
				desc.MediaType == images.MediaTypeDockerSchema2LayerGzip) &&
				desc.Annotations[nydusify.LayerAnnotationNydusBootstrap] == "true" {
				bootstrapDesc = &desc
			}
		} else {
			// Found nydus blob layer, copy to local content store for later use.
			key := fmt.Sprintf("write-nydus-chunk-dict-blob-%s", desc.Digest)
			_, err := g.Do(ctx, key, func(ctx context.Context) (interface{}, error) {
				blobReader, err := fetcher.Fetch(ctx, desc)
				if err != nil {
					return nil, errors.Wrapf(err, "fetch chunk dict blob layer")
				}
				defer blobReader.Close()
				csWriter, err := cs.Writer(ctx, content.WithRef(key))
				if err != nil {
					return nil, errors.Wrap(err, "get content store writer")
				}
				defer csWriter.Close()
				if err := content.Copy(ctx, csWriter, blobReader, desc.Size, desc.Digest); err != nil {
					return nil, errors.Wrap(err, "copy to content store")
				}
				return nil, nil
			})
			if err != nil {
				return "", "", errors.Wrap(err, "pull chunk dict blob to local")
			}
		}
	}
	if bootstrapDesc == nil {
		return "", "", fmt.Errorf("not found bootstrap layer in %s", chunkDictRef)
	}

	bootstrapReader, err := fetcher.Fetch(ctx, *bootstrapDesc)
	if err != nil {
		return "", "", errors.Wrapf(err, "fetch bootstrap")
	}
	defer bootstrapReader.Close()

	bootstrapFile, err := ioutil.TempFile(workDir, "chunk-dict-bootstrap-")
	if err != nil {
		return "", "", errors.Wrapf(err, "create temp file")
	}
	defer bootstrapFile.Close()
	if err := nydusutil.UnpackFile(bootstrapReader, nydusify.BootstrapFileNameInLayer, bootstrapFile); err != nil {
		return "", "", errors.Wrap(err, "unpack Nydus bootstrap layer")
	}

	return bootstrapFile.Name(), bootstrapDesc.Digest, nil
}

func checkChunkDictRef(chunkDictRef string) error {
	_, err := dockerref.ParseDockerRef(chunkDictRef)
	if err != nil {
		return errors.Wrapf(err, "invalid image reference: %s", chunkDictRef)
	}

	return nil
}
