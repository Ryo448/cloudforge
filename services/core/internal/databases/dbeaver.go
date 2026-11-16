package databases

import "errors"

type DBeaverConnection struct {
	Name     string `json:"name"`
	Engine   string `json:"engine"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	User     string `json:"user"`
	Password string `json:"password"`
	CA       string `json:"ca"`
}

// DBeaverConnection is used only by the administrative desktop endpoint.
func (m *Manager) DBeaverConnection(owner, id string) (DBeaverConnection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.get(owner, id)
	if err != nil {
		return DBeaverConnection{}, err
	}
	if r.Engine != "postgresql" && r.Engine != "mysql" {
		return DBeaverConnection{}, errors.New("esta tecnologia não está disponível no DBeaver Community")
	}
	if r.State != "running" || !m.tlsAvailable(r) {
		return DBeaverConnection{}, errors.New("inicie o banco e aguarde a validação do TLS")
	}
	s, err := m.secrets(r)
	if err != nil {
		return DBeaverConnection{}, err
	}
	if s.TLS == nil || len(s.TLS.CA) == 0 || s.Password == "" {
		return DBeaverConnection{}, ErrConflict
	}
	return DBeaverConnection{
		Name: r.Name, Engine: r.Engine, Host: containerName(r), Port: engines[r.Engine].Port,
		Database: DatabaseName, User: Username, Password: s.Password, CA: string(s.TLS.CA),
	}, nil
}
