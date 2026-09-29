package acctest_test

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	accesspb "git.authwise.com/authwise/apis/authwise/access/v1alpha1"
	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

// fakeAccessServer is the in-memory Access service for the acceptance tier.
// It reproduces the two keying styles kit uses: AccessPermission and
// AccessRole are keyed by the name the create request carries in the entity
// (the caller-named lane), while AccessBinding is keyed by a generated AWID.
//
// It deliberately does NOT validate a binding's role_name — kit does not
// either, which is the silent failure the provider's own check covers.
type fakeAccessServer struct {
	accesspb.UnimplementedAuthwiseAccessServiceServer

	mu          sync.Mutex
	permissions map[string]*corepb.AccessPermission
	roles       map[string]*corepb.AccessRole
	rolePerms   map[string]map[string]bool
	bindings    map[string]*corepb.AccessBinding
	seq         int
}

func newFakeAccessServer() *fakeAccessServer {
	return &fakeAccessServer{
		permissions: map[string]*corepb.AccessPermission{},
		roles:       map[string]*corepb.AccessRole{},
		rolePerms:   map[string]map[string]bool{},
		bindings:    map[string]*corepb.AccessBinding{},
	}
}

func (f *fakeAccessServer) nextID(prefix string) string {
	f.seq++
	return fmt.Sprintf("%s-%08d", prefix, f.seq)
}

// callerName composes the full resource name from the parent and the id the
// caller supplied in the entity's name field.
func callerName(parent, collection, id string) (string, error) {
	if id == "" {
		return "", status.Errorf(codes.InvalidArgument, "%s name is required", collection)
	}
	return parent + "/" + collection + "/" + id, nil
}

// --- AccessPermission (caller-named) ---

func (f *fakeAccessServer) GetAccessPermission(_ context.Context, in *accesspb.GetAccessPermissionRequest) (*corepb.AccessPermission, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.permissions[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "access permission %q not found", in.GetName())
	}
	return proto.Clone(p).(*corepb.AccessPermission), nil
}

func (f *fakeAccessServer) CreateAccessPermission(_ context.Context, in *accesspb.CreateAccessPermissionRequest) (*corepb.AccessPermission, error) {

	f.mu.Lock()
	defer f.mu.Unlock()

	name, err := callerName(in.GetParent(), "access-permissions", in.GetAccessPermission().GetName())
	if err != nil {
		return nil, err
	}
	if _, exists := f.permissions[name]; exists {
		return nil, status.Errorf(codes.AlreadyExists, "access permission %q already exists", name)
	}

	p := proto.Clone(in.GetAccessPermission()).(*corepb.AccessPermission)
	p.Name = name
	f.permissions[name] = p

	return proto.Clone(p).(*corepb.AccessPermission), nil
}

func (f *fakeAccessServer) PatchAccessPermission(_ context.Context, in *accesspb.PatchAccessPermissionRequest) (*corepb.AccessPermission, error) {

	f.mu.Lock()
	defer f.mu.Unlock()

	existing, ok := f.permissions[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "access permission %q not found", in.GetName())
	}

	for _, path := range in.GetUpdateMask().GetPaths() {
		switch path {
		case "service":
			existing.Service = in.GetAccessPermission().GetService()
		case "resource_type":
			existing.ResourceType = in.GetAccessPermission().GetResourceType()
		case "description":
			existing.Description = in.GetAccessPermission().GetDescription()
		default:
			return nil, status.Errorf(codes.InvalidArgument, "unsupported update_mask path %q", path)
		}
	}

	return proto.Clone(existing).(*corepb.AccessPermission), nil
}

func (f *fakeAccessServer) DeleteAccessPermission(_ context.Context, in *accesspb.DeleteAccessPermissionRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.permissions[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "access permission %q not found", in.GetName())
	}
	delete(f.permissions, in.GetName())
	return &emptypb.Empty{}, nil
}

// --- AccessRole (caller-named) ---

func (f *fakeAccessServer) GetAccessRole(_ context.Context, in *accesspb.GetAccessRoleRequest) (*corepb.AccessRole, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.roles[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "access role %q not found", in.GetName())
	}
	return proto.Clone(r).(*corepb.AccessRole), nil
}

func (f *fakeAccessServer) CreateAccessRole(_ context.Context, in *accesspb.CreateAccessRoleRequest) (*corepb.AccessRole, error) {

	f.mu.Lock()
	defer f.mu.Unlock()

	name, err := callerName(in.GetParent(), "access-roles", in.GetAccessRole().GetName())
	if err != nil {
		return nil, err
	}
	if _, exists := f.roles[name]; exists {
		return nil, status.Errorf(codes.AlreadyExists, "access role %q already exists", name)
	}

	r := proto.Clone(in.GetAccessRole()).(*corepb.AccessRole)
	r.Name = name
	f.roles[name] = r

	return proto.Clone(r).(*corepb.AccessRole), nil
}

func (f *fakeAccessServer) PatchAccessRole(_ context.Context, in *accesspb.PatchAccessRoleRequest) (*corepb.AccessRole, error) {

	f.mu.Lock()
	defer f.mu.Unlock()

	existing, ok := f.roles[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "access role %q not found", in.GetName())
	}

	for _, path := range in.GetUpdateMask().GetPaths() {
		switch path {
		case "description":
			existing.Description = in.GetAccessRole().GetDescription()
		default:
			return nil, status.Errorf(codes.InvalidArgument, "unsupported update_mask path %q", path)
		}
	}

	return proto.Clone(existing).(*corepb.AccessRole), nil
}

func (f *fakeAccessServer) DeleteAccessRole(_ context.Context, in *accesspb.DeleteAccessRoleRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.roles[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "access role %q not found", in.GetName())
	}
	delete(f.roles, in.GetName())
	return &emptypb.Empty{}, nil
}

// --- AccessRole ↔ AccessPermission association ---

func (f *fakeAccessServer) AssociateAccessPermissionsToAccessRole(_ context.Context, in *accesspb.AssociateAccessPermissionsToAccessRoleRequest) (*emptypb.Empty, error) {

	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.roles[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "access role %q not found", in.GetName())
	}
	if f.rolePerms[in.GetName()] == nil {
		f.rolePerms[in.GetName()] = map[string]bool{}
	}

	for _, n := range in.GetAssociation().GetSet() {
		f.rolePerms[in.GetName()][n] = true
	}
	for _, n := range in.GetAssociation().GetRemove() {
		delete(f.rolePerms[in.GetName()], n)
	}

	return &emptypb.Empty{}, nil
}

// ListAccessPermissionsByAccessRole answers with the edges of the role's own
// audience — the behavior kit v1.10.0 fixed in kit#312, and the behavior the
// association resource's refresh depends on. Pages one at a time.
func (f *fakeAccessServer) ListAccessPermissionsByAccessRole(_ context.Context, in *accesspb.ListAccessPermissionsByAccessRoleRequest) (*accesspb.ListAccessPermissionsByAccessRoleResponse, error) {

	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.roles[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "access role %q not found", in.GetName())
	}

	names := f.rolePermissionsLocked(in.GetName())
	start := 0
	if in.GetPageToken() != "" {
		fmt.Sscanf(in.GetPageToken(), "%d", &start)
	}

	res := &accesspb.ListAccessPermissionsByAccessRoleResponse{}
	if start < len(names) {
		res.AccessPermissions = []*corepb.AccessPermission{{Name: names[start]}}
		if start+1 < len(names) {
			res.NextPageToken = fmt.Sprintf("%d", start+1)
		}
	}

	return res, nil
}

// rolePermissionsLocked returns the role's permission names scoped to the
// role's own audience, sorted; the caller holds f.mu.
func (f *fakeAccessServer) rolePermissionsLocked(role string) []string {

	audience := role[:strings.LastIndex(role, "/access-roles/")]

	names := make([]string, 0, len(f.rolePerms[role]))
	for n := range f.rolePerms[role] {
		if strings.HasPrefix(n, audience+"/") {
			names = append(names, n)
		}
	}
	sort.Strings(names)

	return names
}

// rolePermissions is the test-facing variant of rolePermissionsLocked.
func (f *fakeAccessServer) rolePermissions(role string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rolePermissionsLocked(role)
}

// --- AccessBinding (AWID-keyed) ---

func (f *fakeAccessServer) GetAccessBinding(_ context.Context, in *accesspb.GetAccessBindingRequest) (*corepb.AccessBinding, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.bindings[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "access binding %q not found", in.GetName())
	}
	return proto.Clone(b).(*corepb.AccessBinding), nil
}

// grantKey is a binding's identity since kit#616: audience, subject, role,
// resource and condition. expires_at is deliberately not part of it — the
// same grant with a different expiry is the same grant.
func grantKey(audience string, b *corepb.AccessBinding) string {
	return strings.Join([]string{audience, b.GetSubjectType(), b.GetSubjectId(), b.GetRoleName(),
		b.GetResourceType(), b.GetResourceId(), b.GetConditionId()}, "\x00")
}

// grantHolderLocked returns the name of another binding in the audience
// holding the same grant, or "". The caller holds f.mu.
func (f *fakeAccessServer) grantHolderLocked(audience, self string, b *corepb.AccessBinding) string {
	key := grantKey(audience, b)
	for name, other := range f.bindings {
		if name != self && strings.HasPrefix(name, audience+"/access-bindings/") && grantKey(audience, other) == key {
			return name
		}
	}
	return ""
}

// CreateAccessBinding stores the row as given: kit accepts a role_name that
// resolves to nothing, which is exactly why the provider checks it first.
// It refuses a grant the audience already holds (kit#616), whatever the
// expiry.
func (f *fakeAccessServer) CreateAccessBinding(_ context.Context, in *accesspb.CreateAccessBindingRequest) (*corepb.AccessBinding, error) {

	f.mu.Lock()
	defer f.mu.Unlock()

	if holder := f.grantHolderLocked(in.GetParent(), "", in.GetAccessBinding()); holder != "" {
		return nil, status.Errorf(codes.AlreadyExists, "access binding %s already grants this", holder)
	}

	b := proto.Clone(in.GetAccessBinding()).(*corepb.AccessBinding)
	b.Name = in.GetParent() + "/access-bindings/" + f.nextID("axb")
	b.CreatedBy = "acc-test"
	f.bindings[b.GetName()] = b

	return proto.Clone(b).(*corepb.AccessBinding), nil
}

func (f *fakeAccessServer) PatchAccessBinding(_ context.Context, in *accesspb.PatchAccessBindingRequest) (*corepb.AccessBinding, error) {

	f.mu.Lock()
	defer f.mu.Unlock()

	stored, ok := f.bindings[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "access binding %q not found", in.GetName())
	}

	// The patch is applied to a copy and checked before it is stored, so a
	// refused update leaves the row as it was.
	existing := proto.Clone(stored).(*corepb.AccessBinding)
	for _, path := range in.GetUpdateMask().GetPaths() {
		switch path {
		case "subject_type":
			existing.SubjectType = in.GetAccessBinding().GetSubjectType()
		case "subject_id":
			existing.SubjectId = in.GetAccessBinding().GetSubjectId()
		case "role_name":
			existing.RoleName = in.GetAccessBinding().GetRoleName()
		case "resource_type":
			existing.ResourceType = in.GetAccessBinding().GetResourceType()
		case "resource_id":
			existing.ResourceId = in.GetAccessBinding().GetResourceId()
		case "condition_id":
			existing.ConditionId = in.GetAccessBinding().GetConditionId()
		case "expires_at":
			existing.ExpiresAt = in.GetAccessBinding().GetExpiresAt()
		default:
			return nil, status.Errorf(codes.InvalidArgument, "unsupported update_mask path %q", path)
		}
	}

	audience, _, _ := strings.Cut(in.GetName(), "/access-bindings/")
	if holder := f.grantHolderLocked(audience, in.GetName(), existing); holder != "" {
		return nil, status.Errorf(codes.AlreadyExists, "access binding %s already grants this", holder)
	}
	f.bindings[in.GetName()] = existing

	return proto.Clone(existing).(*corepb.AccessBinding), nil
}

func (f *fakeAccessServer) DeleteAccessBinding(_ context.Context, in *accesspb.DeleteAccessBindingRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.bindings[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "access binding %q not found", in.GetName())
	}
	delete(f.bindings, in.GetName())
	return &emptypb.Empty{}, nil
}

// onlyBinding returns the single binding on the fake server.
func (f *fakeAccessServer) onlyBinding() *corepb.AccessBinding {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, b := range f.bindings {
		return proto.Clone(b).(*corepb.AccessBinding)
	}
	return nil
}
