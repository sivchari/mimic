package model

import "time"

type User struct {
	ID   string
	Name string
}

type Order struct{ ID string }
type Item struct{ ID string }
type Event struct{ At time.Time }

type UserID string
type OrderID string
type ItemID string
