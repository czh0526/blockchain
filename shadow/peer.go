package shadow

type PeerInfo struct {
	Version int `json:"version"`
}

type peer struct {
	id      string
	version int
}

func (p *peer) Info() *PeerInfo {
	return &PeerInfo{
		Version: p.version,
	}
}
