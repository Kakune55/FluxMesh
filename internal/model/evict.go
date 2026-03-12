package model

type NodeEvictResult struct {
	NodeID        string `json:"node_id"`
	NodeRole      string `json:"node_role"`
	NodeDeleted   bool   `json:"node_deleted"`
	MemberID      uint64 `json:"member_id,omitempty"`
	MemberRemoved bool   `json:"member_removed"`
}
