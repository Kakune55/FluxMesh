package model

type ClusterStatus struct {
	Endpoint         string                `json:"endpoint"`
	ClusterID        uint64                `json:"cluster_id"`
	CurrentMemberID  uint64                `json:"current_member_id"`
	LeaderID         uint64                `json:"leader_id"`
	RaftTerm         uint64                `json:"raft_term"`
	RaftIndex        uint64                `json:"raft_index"`
	RaftAppliedIndex uint64                `json:"raft_applied_index"`
	DBSize           int64                 `json:"db_size"`
	Members          []ClusterMemberStatus `json:"members"`
}

type ClusterMemberStatus struct {
	ID         uint64   `json:"id"`
	Name       string   `json:"name"`
	Role       string   `json:"role"`
	IsLearner  bool     `json:"is_learner"`
	PeerURLs   []string `json:"peer_urls"`
	ClientURLs []string `json:"client_urls"`
}
