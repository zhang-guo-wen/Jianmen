package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"jianmen/internal/model"
)

func TestParseApplicationAddress(t *testing.T) {
	tests := []struct {
		name      string
		address   string
		want      ApplicationAddress
		wantError bool
	}{
		{
			name:    "Nacos address with fragment route",
			address: "http://47.121.184.68:18848/nacos/#/login?namespace=&pageSize=&pageNo=",
			want: ApplicationAddress{
				Address:   "http://47.121.184.68:18848/nacos/#/login?namespace=&pageSize=&pageNo=",
				EntryPath: "/nacos/#/login?namespace=&pageSize=&pageNo=",
				Scheme:    "http",
				Host:      "47.121.184.68",
				Port:      18848,
			},
		},
		{
			name:    "HTTPS default port and root path",
			address: "https://console.example.com",
			want: ApplicationAddress{
				Address:   "https://console.example.com/",
				EntryPath: "/",
				Scheme:    "https",
				Host:      "console.example.com",
				Port:      443,
			},
		},
		{name: "missing scheme", address: "console.example.com/path", wantError: true},
		{name: "unsupported scheme", address: "ftp://console.example.com/path", wantError: true},
		{name: "credentials rejected", address: "http://user:pass@console.example.com/", wantError: true},
		{name: "invalid port", address: "http://console.example.com:70000/", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseApplicationAddress(tt.address)
			if tt.wantError {
				if err == nil {
					t.Fatalf("ParseApplicationAddress(%q) succeeded: %#v", tt.address, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseApplicationAddress(%q): %v", tt.address, err)
			}
			if got != tt.want {
				t.Fatalf("ParseApplicationAddress(%q) = %#v, want %#v", tt.address, got, tt.want)
			}
		})
	}
}

type applicationServiceRepository struct {
	applications  map[string]model.Application
	createHook    func()
	deleteContext context.Context
	deleteCtxErr  error
	getCalls      int
}

func (r *applicationServiceRepository) ListApplications(context.Context) []model.Application {
	result := make([]model.Application, 0, len(r.applications))
	for _, application := range r.applications {
		result = append(result, application)
	}
	return result
}

func (r *applicationServiceRepository) GetApplication(_ context.Context, id string) (model.Application, error) {
	r.getCalls++
	application, ok := r.applications[id]
	if !ok {
		return model.Application{}, errors.New("not found")
	}
	return application, nil
}

func (r *applicationServiceRepository) CreateManagedApplication(
	_ context.Context,
	application model.Application,
	_ string,
) (model.Application, error) {
	application.ID = "app-created"
	application.CreatedAt = time.Now().UTC()
	application.UpdatedAt = application.CreatedAt
	r.applications[application.ID] = application
	if r.createHook != nil {
		r.createHook()
	}
	return application, nil
}

func (r *applicationServiceRepository) UpdateManagedApplication(
	_ context.Context,
	id string,
	application model.Application,
) (model.Application, error) {
	application.ID = id
	application.UpdatedAt = time.Now().UTC()
	r.applications[id] = application
	return application, nil
}

func (r *applicationServiceRepository) DeleteManagedApplication(ctx context.Context, id string) error {
	r.deleteContext = ctx
	r.deleteCtxErr = ctx.Err()
	if err := ctx.Err(); err != nil {
		return err
	}
	delete(r.applications, id)
	return nil
}

type applicationServiceAuthorizer struct {
	decisions []AuthorizationDecision
	err       error
}

func (a applicationServiceAuthorizer) AuthorizeBatch(
	_ context.Context,
	_ string,
	requests []AuthorizationRequest,
) ([]AuthorizationDecision, error) {
	if a.err != nil {
		return nil, a.err
	}
	if a.decisions != nil {
		return a.decisions, nil
	}
	decisions := make([]AuthorizationDecision, len(requests))
	for i := range decisions {
		decisions[i].Allowed = true
	}
	return decisions, nil
}

type applicationServiceProxy struct {
	active      map[int]model.Application
	addErr      error
	updateErr   error
	updateCalls int
	removeCalls int
}

func (p *applicationServiceProxy) AddProxy(application model.Application) error {
	if p.addErr != nil {
		return p.addErr
	}
	p.active[application.ListenPort] = application
	return nil
}

func (p *applicationServiceProxy) UpdateProxy(previousPort int, application model.Application) error {
	p.updateCalls++
	delete(p.active, previousPort)
	if p.updateErr != nil {
		return p.updateErr
	}
	p.active[application.ListenPort] = application
	return nil
}

func (p *applicationServiceProxy) RemoveProxy(port int) {
	p.removeCalls++
	delete(p.active, port)
}

func TestApplicationServiceListFailsClosed(t *testing.T) {
	repository := &applicationServiceRepository{applications: map[string]model.Application{
		"visible": {ID: "visible", Name: "visible", Status: "active"},
		"hidden":  {ID: "hidden", Name: "hidden", Status: "active"},
	}}
	service, err := NewApplicationService(
		repository,
		applicationServiceAuthorizer{err: errors.New("authorization unavailable")},
		nil,
		47110,
		47199,
	)
	if err != nil {
		t.Fatalf("new application service: %v", err)
	}
	if applications, err := service.List(context.Background(), ApplicationActor{UserID: "user-1"}); err == nil || applications != nil {
		t.Fatalf("List() = %#v, %v; want authorization failure and no data", applications, err)
	}
}

func TestApplicationServiceDeniedGetDoesNotReadRepository(t *testing.T) {
	repository := &applicationServiceRepository{applications: map[string]model.Application{
		"app-1": {ID: "app-1", Name: "secret", Status: "active"},
	}}
	service, err := NewApplicationService(
		repository,
		applicationServiceAuthorizer{decisions: []AuthorizationDecision{{Allowed: false}}},
		nil,
		47110,
		47199,
	)
	if err != nil {
		t.Fatalf("new application service: %v", err)
	}
	_, err = service.Get(context.Background(), ApplicationActor{UserID: "user-1"}, "app-1")
	if !errors.Is(err, ErrApplicationForbidden) {
		t.Fatalf("Get() error = %v, want %v", err, ErrApplicationForbidden)
	}
	if repository.getCalls != 0 {
		t.Fatalf("repository reads = %d, want 0 before authorization", repository.getCalls)
	}
}

func TestApplicationServiceCreateCancellationUsesDetachedCompensation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	repository := &applicationServiceRepository{applications: make(map[string]model.Application), createHook: cancel}
	service, err := NewApplicationService(
		repository,
		applicationServiceAuthorizer{},
		nil,
		47110,
		47199,
	)
	if err != nil {
		t.Fatalf("new application service: %v", err)
	}
	_, err = service.Create(ctx, ApplicationActor{UserID: "user-1"}, ApplicationRequest{Address: "http://127.0.0.1:8080"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Create() error = %v, want context canceled", err)
	}
	if len(repository.applications) != 0 {
		t.Fatalf("applications after cancellation = %#v, want empty", repository.applications)
	}
	if repository.deleteContext == nil || repository.deleteCtxErr != nil {
		t.Fatalf("compensation context error at delete = %v, want nil", repository.deleteCtxErr)
	}
}

func TestApplicationServiceCreateProxyFailureDeletesApplication(t *testing.T) {
	repository := &applicationServiceRepository{applications: make(map[string]model.Application)}
	proxy := &applicationServiceProxy{active: make(map[int]model.Application), addErr: errors.New("listen failed")}
	service, err := NewApplicationService(repository, applicationServiceAuthorizer{}, proxy, 47110, 47199)
	if err != nil {
		t.Fatalf("new application service: %v", err)
	}
	_, err = service.Create(
		context.Background(),
		ApplicationActor{UserID: "admin", SuperAdmin: true},
		ApplicationRequest{Address: "http://127.0.0.1:8080", Status: "active"},
	)
	if !errors.Is(err, ErrApplicationRuntime) {
		t.Fatalf("Create() error = %v, want %v", err, ErrApplicationRuntime)
	}
	if len(repository.applications) != 0 {
		t.Fatalf("applications after proxy failure = %#v, want empty", repository.applications)
	}
}

func TestApplicationServiceUpdateProxyFailureRestoresDatabaseAndRuntime(t *testing.T) {
	previous := model.Application{
		ID: "app-1", Name: "before", Address: "http://127.0.0.1:8080/", EntryPath: "/",
		InternalScheme: "http", InternalHost: "127.0.0.1", InternalPort: 8080,
		ListenPort: 47110, Status: "active",
	}
	repository := &applicationServiceRepository{applications: map[string]model.Application{previous.ID: previous}}
	proxy := &applicationServiceProxy{
		active:    map[int]model.Application{previous.ListenPort: previous},
		updateErr: errors.New("replacement failed"),
	}
	service, err := NewApplicationService(repository, applicationServiceAuthorizer{}, proxy, 47110, 47199)
	if err != nil {
		t.Fatalf("new application service: %v", err)
	}
	_, err = service.Update(
		context.Background(),
		ApplicationActor{UserID: "admin", SuperAdmin: true},
		previous.ID,
		ApplicationRequest{Address: "http://127.0.0.2:8081/", ListenPort: previous.ListenPort, Status: "active"},
	)
	if !errors.Is(err, ErrApplicationRuntime) {
		t.Fatalf("Update() error = %v, want %v", err, ErrApplicationRuntime)
	}
	stored := repository.applications[previous.ID]
	if stored.InternalHost != previous.InternalHost || stored.InternalPort != previous.InternalPort {
		t.Fatalf("stored application = %#v, want previous %#v", stored, previous)
	}
	running, ok := proxy.active[previous.ListenPort]
	if !ok || running.InternalHost != previous.InternalHost {
		t.Fatalf("running application = %#v, %v; want restored previous proxy", running, ok)
	}
}

func TestApplicationServiceUpdateInheritsInactiveStatusWhenOmitted(t *testing.T) {
	previous := model.Application{
		ID: "app-inactive", Name: "before", Address: "http://127.0.0.1:8080/", EntryPath: "/",
		InternalScheme: "http", InternalHost: "127.0.0.1", InternalPort: 8080,
		ListenPort: 47110, Status: "inactive",
	}
	repository := &applicationServiceRepository{applications: map[string]model.Application{previous.ID: previous}}
	proxy := &applicationServiceProxy{active: make(map[int]model.Application)}
	service, err := NewApplicationService(repository, applicationServiceAuthorizer{}, proxy, 47110, 47199)
	if err != nil {
		t.Fatalf("new application service: %v", err)
	}
	updated, err := service.Update(
		context.Background(),
		ApplicationActor{UserID: "admin", SuperAdmin: true},
		previous.ID,
		ApplicationRequest{Address: "http://127.0.0.2:8081/"},
	)
	if err != nil {
		t.Fatalf("Update(): %v", err)
	}
	if updated.Status != "inactive" || repository.applications[previous.ID].Status != "inactive" {
		t.Fatalf("updated status = %q, stored status = %q; want inactive", updated.Status, repository.applications[previous.ID].Status)
	}
	if proxy.updateCalls != 0 || len(proxy.active) != 0 {
		t.Fatalf("proxy unexpectedly started: update calls=%d active=%#v", proxy.updateCalls, proxy.active)
	}
}

func TestApplicationServiceCreateDefaultsEmptyStatusToActive(t *testing.T) {
	repository := &applicationServiceRepository{applications: make(map[string]model.Application)}
	service, err := NewApplicationService(repository, applicationServiceAuthorizer{}, nil, 47110, 47199)
	if err != nil {
		t.Fatalf("new application service: %v", err)
	}
	created, err := service.Create(
		context.Background(),
		ApplicationActor{UserID: "admin", SuperAdmin: true},
		ApplicationRequest{Address: "http://127.0.0.1:8080/"},
	)
	if err != nil {
		t.Fatalf("Create(): %v", err)
	}
	if created.Status != "active" || repository.applications[created.ID].Status != "active" {
		t.Fatalf("created status = %q, stored status = %q; want active", created.Status, repository.applications[created.ID].Status)
	}
}

type serializedApplicationRepository struct {
	mu            sync.Mutex
	application   model.Application
	exists        bool
	deleteEntered chan struct{}
	deleteOnce    sync.Once
}

func (r *serializedApplicationRepository) ListApplications(context.Context) []model.Application {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.exists {
		return nil
	}
	return []model.Application{r.application}
}

func (r *serializedApplicationRepository) GetApplication(_ context.Context, id string) (model.Application, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.exists || r.application.ID != id {
		return model.Application{}, errors.New("application not found")
	}
	return r.application, nil
}

func (r *serializedApplicationRepository) CreateManagedApplication(
	_ context.Context,
	application model.Application,
	_ string,
) (model.Application, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.application = application
	r.exists = true
	return application, nil
}

func (r *serializedApplicationRepository) UpdateManagedApplication(
	_ context.Context,
	id string,
	application model.Application,
) (model.Application, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.exists || r.application.ID != id {
		return model.Application{}, errors.New("application not found")
	}
	application.ID = id
	r.application = application
	return application, nil
}

func (r *serializedApplicationRepository) DeleteManagedApplication(_ context.Context, id string) error {
	r.deleteOnce.Do(func() { close(r.deleteEntered) })
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.exists || r.application.ID != id {
		return errors.New("application not found")
	}
	r.exists = false
	return nil
}

func (r *serializedApplicationRepository) snapshot() (model.Application, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.application, r.exists
}

type serializedApplicationProxy struct {
	mu                  sync.Mutex
	active              map[int]model.Application
	firstUpdateEntered  chan struct{}
	secondUpdateEntered chan struct{}
	releaseFirstUpdate  chan struct{}
	updateCalls         int
}

func (p *serializedApplicationProxy) AddProxy(application model.Application) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.active[application.ListenPort] = application
	return nil
}

func (p *serializedApplicationProxy) UpdateProxy(previousPort int, application model.Application) error {
	p.mu.Lock()
	p.updateCalls++
	call := p.updateCalls
	if call == 1 {
		close(p.firstUpdateEntered)
	} else {
		select {
		case <-p.secondUpdateEntered:
		default:
			close(p.secondUpdateEntered)
		}
	}
	p.mu.Unlock()
	if call == 1 {
		<-p.releaseFirstUpdate
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.active, previousPort)
	p.active[application.ListenPort] = application
	return nil
}

func (p *serializedApplicationProxy) RemoveProxy(port int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.active, port)
}

func (p *serializedApplicationProxy) snapshot(port int) (model.Application, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	application, ok := p.active[port]
	return application, ok
}

func newSerializedApplicationFixture() (*serializedApplicationRepository, *serializedApplicationProxy, model.Application) {
	application := model.Application{
		ID: "app-serialized", Name: "initial", Address: "http://127.0.0.1:8080/", EntryPath: "/",
		InternalScheme: "http", InternalHost: "127.0.0.1", InternalPort: 8080,
		ListenPort: 47110, Status: "active",
	}
	repository := &serializedApplicationRepository{
		application: application, exists: true, deleteEntered: make(chan struct{}),
	}
	proxy := &serializedApplicationProxy{
		active:              map[int]model.Application{application.ListenPort: application},
		firstUpdateEntered:  make(chan struct{}),
		secondUpdateEntered: make(chan struct{}),
		releaseFirstUpdate:  make(chan struct{}),
	}
	return repository, proxy, application
}

func TestApplicationServiceSerializesConcurrentUpdates(t *testing.T) {
	repository, proxy, application := newSerializedApplicationFixture()
	service, err := NewApplicationService(repository, applicationServiceAuthorizer{}, proxy, 47110, 47199)
	if err != nil {
		t.Fatalf("new application service: %v", err)
	}
	firstDone := make(chan error, 1)
	go func() {
		_, updateErr := service.Update(context.Background(), ApplicationActor{UserID: "admin", SuperAdmin: true}, application.ID,
			ApplicationRequest{Name: "first", Address: "http://127.0.0.2:8081/", Status: "active"})
		firstDone <- updateErr
	}()
	waitApplicationSignal(t, proxy.firstUpdateEntered, "first proxy update")

	secondDone := make(chan error, 1)
	go func() {
		_, updateErr := service.Update(context.Background(), ApplicationActor{UserID: "admin", SuperAdmin: true}, application.ID,
			ApplicationRequest{Name: "second", Address: "http://127.0.0.3:8082/", Status: "active"})
		secondDone <- updateErr
	}()
	interleaved := applicationSignalWithin(proxy.secondUpdateEntered, 100*time.Millisecond)
	close(proxy.releaseFirstUpdate)
	if err := <-firstDone; err != nil {
		t.Fatalf("first Update(): %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second Update(): %v", err)
	}
	if interleaved {
		t.Fatal("second update reached proxy before first mutation completed")
	}
	stored, exists := repository.snapshot()
	running, runningExists := proxy.snapshot(application.ListenPort)
	if !exists || !runningExists || stored.Name != "second" || running.Name != stored.Name || running.InternalHost != stored.InternalHost {
		t.Fatalf("final DB/proxy mismatch: stored=%#v exists=%v running=%#v exists=%v", stored, exists, running, runningExists)
	}
}

func TestApplicationServiceSerializesUpdateAndDelete(t *testing.T) {
	repository, proxy, application := newSerializedApplicationFixture()
	service, err := NewApplicationService(repository, applicationServiceAuthorizer{}, proxy, 47110, 47199)
	if err != nil {
		t.Fatalf("new application service: %v", err)
	}
	updateDone := make(chan error, 1)
	go func() {
		_, updateErr := service.Update(context.Background(), ApplicationActor{UserID: "admin", SuperAdmin: true}, application.ID,
			ApplicationRequest{Name: "updated", Address: "http://127.0.0.2:8081/", Status: "active"})
		updateDone <- updateErr
	}()
	waitApplicationSignal(t, proxy.firstUpdateEntered, "proxy update")

	deleteDone := make(chan error, 1)
	go func() {
		deleteDone <- service.Delete(context.Background(), ApplicationActor{UserID: "admin", SuperAdmin: true}, application.ID)
	}()
	interleaved := applicationSignalWithin(repository.deleteEntered, 100*time.Millisecond)
	close(proxy.releaseFirstUpdate)
	if err := <-updateDone; err != nil {
		t.Fatalf("Update(): %v", err)
	}
	if err := <-deleteDone; err != nil {
		t.Fatalf("Delete(): %v", err)
	}
	if interleaved {
		t.Fatal("delete reached repository before update mutation completed")
	}
	_, exists := repository.snapshot()
	_, running := proxy.snapshot(application.ListenPort)
	if exists || running {
		t.Fatalf("update/delete left orphan state: database exists=%v proxy exists=%v", exists, running)
	}
}

func waitApplicationSignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}

func applicationSignalWithin(signal <-chan struct{}, timeout time.Duration) bool {
	select {
	case <-signal:
		return true
	case <-time.After(timeout):
		return false
	}
}
