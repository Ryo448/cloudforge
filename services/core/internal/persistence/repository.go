package persistence

import "cloudforge/core/internal/model"

// Repository is the storage boundary. A PostgreSQL adapter can implement this contract
// without changing process supervision, HTTP handlers or authentication policy.
type Repository interface {
	AuthRepository
	Setting(string, any) error
	Set(string, any) error
	Projects() ([]model.Project, error)
	Project(string) (model.Project, error)
	SaveProject(model.Project) error
	DeleteProject(string) error
	SaveProjectPort(model.Project) error
	Desired(string, string) error
	Runtime(model.Runtime) error
	PreviousRuntime(string) (model.Runtime, error)
	Dependencies(string) (model.Dependencies, error)
	SaveDependencies(model.Dependencies) error
	Routes() ([]model.Route, error)
	SaveRoute(model.Route) error
	DeleteRoute(string) error
	SaveCloudConfig(model.CloudConfig) error
	Operation(string, string, string, any, string) error
	PendingOperations() ([]map[string]any, error)
}

var _ Repository = (*Store)(nil)
