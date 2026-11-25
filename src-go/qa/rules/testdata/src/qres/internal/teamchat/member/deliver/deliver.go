// Package deliver stands for teamchat's delivery: an item and the question it shows.
package deliver

import "qres/internal/teamchat/proto"

// Item is one pending item to render.
type Item struct {
	ID       string
	Question *proto.EntryQuestion
}
