package bsky

type Session struct {
	AccessJwt string `json:"accessJwt"`
	Did       string `json:"did"`
}

type NotificationResponse struct {
	Notifications []Notification `json:"notifications"`
}

type Notification struct {
	Uri      string `json:"uri"`
	Cid      string `json:"cid"`
	Author   Author `json:"author"`
	Reason   string `json:"reason"`
	Record   Record `json:"record"`
	IndexedAt string `json:"indexedAt"`
}

type Author struct {
	Handle string `json:"handle"`
}

type Record struct {
	Text      string `json:"text,omitempty"`
	CreatedAt string `json:"createdAt"`
}