package acctest_test

import (
	"context"
	"errors"
	"io"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

// Asset, the row and its blob. kit keeps the two apart: the row goes
// through the CRUD RPCs, the bytes through the UploadAsset client stream
// and the DownloadAsset server stream, each chunk naming the asset.
// RemoveAsset deletes the file and keeps the row (kit#54); a file that is
// already gone is success.

func (f *fakeIdentityServer) GetAsset(ctx context.Context, in *identitypb.GetAssetRequest) (*corepb.Asset, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	a, ok := f.assets[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "asset %q not found", in.GetName())
	}
	return proto.Clone(a).(*corepb.Asset), nil
}

func (f *fakeIdentityServer) CreateAsset(ctx context.Context, in *identitypb.CreateAssetRequest) (*corepb.Asset, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if err := checkParent("asset", tenantParent, in.GetParent()); err != nil {
		return nil, err
	}
	a := proto.Clone(in.GetAsset()).(*corepb.Asset)
	a.Name = in.GetParent() + "/assets/" + f.nextID("as")
	f.assets[a.GetName()] = a
	return proto.Clone(a).(*corepb.Asset), nil
}

func (f *fakeIdentityServer) PatchAsset(ctx context.Context, in *identitypb.PatchAssetRequest) (*corepb.Asset, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	existing, ok := f.assets[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "asset %q not found", in.GetName())
	}
	for _, path := range in.GetUpdateMask().GetPaths() {
		switch path {
		case "display_name":
			existing.DisplayName = in.GetAsset().GetDisplayName()
		case "path":
			existing.Path = in.GetAsset().GetPath()
		case "mime_type":
			existing.MimeType = in.GetAsset().GetMimeType()
		case "labels":
			existing.Labels = in.GetAsset().GetLabels()
		default:
			return nil, status.Errorf(codes.InvalidArgument, "unsupported update_mask path %q", path)
		}
	}
	return proto.Clone(existing).(*corepb.Asset), nil
}

func (f *fakeIdentityServer) DeleteAsset(ctx context.Context, in *identitypb.DeleteAssetRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if _, ok := f.assets[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "asset %q not found", in.GetName())
	}
	delete(f.assets, in.GetName())
	delete(f.blobs, in.GetName())
	return &emptypb.Empty{}, nil
}

// UploadAsset collects the stream and stores it at close, replacing any
// earlier content. Every chunk must name the same, existing asset.
func (f *fakeIdentityServer) UploadAsset(stream identitypb.AuthwiseIdentityService_UploadAssetServer) error {

	var name string
	var data []byte

	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if name != "" && msg.GetName() != name {
			return status.Errorf(codes.InvalidArgument, "chunk names %q, stream began with %q", msg.GetName(), name)
		}
		name = msg.GetName()
		data = append(data, msg.GetData()...)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(stream.Context())
	if _, ok := f.assets[name]; !ok {
		return status.Errorf(codes.NotFound, "asset %q not found", name)
	}
	f.blobs[name] = data
	f.uploads++
	return stream.SendAndClose(&emptypb.Empty{})
}

// DownloadAsset streams the content in small chunks, so a reader has to
// reassemble it. An asset with no content is NotFound, as in kit.
func (f *fakeIdentityServer) DownloadAsset(in *identitypb.DownloadAssetRequest, stream identitypb.AuthwiseIdentityService_DownloadAssetServer) error {

	f.mu.Lock()
	f.recordAuth(stream.Context())
	data, ok := f.blobs[in.GetName()]
	f.mu.Unlock()
	if !ok {
		return status.Errorf(codes.NotFound, "resource not found: %s", in.GetName())
	}

	const chunk = 3
	for off := 0; off < len(data); off += chunk {
		if err := stream.Send(&identitypb.AssetDownloadResponse{Data: data[off:min(off+chunk, len(data))]}); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeIdentityServer) RemoveAsset(ctx context.Context, in *identitypb.RemoveAssetRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	if _, ok := f.assets[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "asset %q not found", in.GetName())
	}
	delete(f.blobs, in.GetName())
	return &emptypb.Empty{}, nil
}

// blob returns an asset's stored content, and whether it has any.
func (f *fakeIdentityServer) blob(name string) ([]byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.blobs[name]
	return b, ok
}

// setBlob replaces an asset's content out of band, as another client would.
func (f *fakeIdentityServer) setBlob(name string, data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.blobs[name] = data
}

func (f *fakeIdentityServer) uploadCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.uploads
}
