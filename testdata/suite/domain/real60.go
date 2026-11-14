package domain

import (
	"context"
	"time"

	"example.com/suite/model"
)

// Real60 is a realistic wide repository: ctx first, error last, ID and entity types repeated.
type Real60 interface {
	GetUser(ctx context.Context, id model.UserID) (*model.User, error)
	CreateUser(ctx context.Context, e *model.User) (*model.User, error)
	UpdateUser(ctx context.Context, e *model.User) error
	DeleteUser(ctx context.Context, id model.UserID) error
	ListUser(ctx context.Context, limit int, cursor string) ([]*model.User, string, error)
	CountUser(ctx context.Context) (int, error)
	ExistsUser(ctx context.Context, id model.UserID) (bool, error)
	FindUserByName(ctx context.Context, name string) (*model.User, error)
	FindUsersByIDs(ctx context.Context, ids []model.UserID) ([]*model.User, error)
	ArchiveUser(ctx context.Context, id model.UserID) error
	RestoreUser(ctx context.Context, id model.UserID) error
	LockUser(ctx context.Context, id model.UserID, ttl time.Duration) error
	UnlockUser(ctx context.Context, id model.UserID) error
	TouchUser(ctx context.Context, id model.UserID, at time.Time) error
	PurgeUsersBefore(ctx context.Context, before time.Time) (int, error)
	RenameUser(ctx context.Context, id model.UserID, name string) error
	SetUserOwner(ctx context.Context, id model.UserID, owner model.UserID) error
	UserHistory(ctx context.Context, id model.UserID) ([]*model.Event, error)
	SearchUser(ctx context.Context, query string, limit int) ([]*model.User, error)
	ValidateUser(ctx context.Context, e *model.User) error
	GetOrder(ctx context.Context, id model.OrderID) (*model.Order, error)
	CreateOrder(ctx context.Context, e *model.Order) (*model.Order, error)
	UpdateOrder(ctx context.Context, e *model.Order) error
	DeleteOrder(ctx context.Context, id model.OrderID) error
	ListOrder(ctx context.Context, limit int, cursor string) ([]*model.Order, string, error)
	CountOrder(ctx context.Context) (int, error)
	ExistsOrder(ctx context.Context, id model.OrderID) (bool, error)
	FindOrderByName(ctx context.Context, name string) (*model.Order, error)
	FindOrdersByIDs(ctx context.Context, ids []model.OrderID) ([]*model.Order, error)
	ArchiveOrder(ctx context.Context, id model.OrderID) error
	RestoreOrder(ctx context.Context, id model.OrderID) error
	LockOrder(ctx context.Context, id model.OrderID, ttl time.Duration) error
	UnlockOrder(ctx context.Context, id model.OrderID) error
	TouchOrder(ctx context.Context, id model.OrderID, at time.Time) error
	PurgeOrdersBefore(ctx context.Context, before time.Time) (int, error)
	RenameOrder(ctx context.Context, id model.OrderID, name string) error
	SetOrderOwner(ctx context.Context, id model.OrderID, owner model.UserID) error
	OrderHistory(ctx context.Context, id model.OrderID) ([]*model.Event, error)
	SearchOrder(ctx context.Context, query string, limit int) ([]*model.Order, error)
	ValidateOrder(ctx context.Context, e *model.Order) error
	GetItem(ctx context.Context, id model.ItemID) (*model.Item, error)
	CreateItem(ctx context.Context, e *model.Item) (*model.Item, error)
	UpdateItem(ctx context.Context, e *model.Item) error
	DeleteItem(ctx context.Context, id model.ItemID) error
	ListItem(ctx context.Context, limit int, cursor string) ([]*model.Item, string, error)
	CountItem(ctx context.Context) (int, error)
	ExistsItem(ctx context.Context, id model.ItemID) (bool, error)
	FindItemByName(ctx context.Context, name string) (*model.Item, error)
	FindItemsByIDs(ctx context.Context, ids []model.ItemID) ([]*model.Item, error)
	ArchiveItem(ctx context.Context, id model.ItemID) error
	RestoreItem(ctx context.Context, id model.ItemID) error
	LockItem(ctx context.Context, id model.ItemID, ttl time.Duration) error
	UnlockItem(ctx context.Context, id model.ItemID) error
	TouchItem(ctx context.Context, id model.ItemID, at time.Time) error
	PurgeItemsBefore(ctx context.Context, before time.Time) (int, error)
	RenameItem(ctx context.Context, id model.ItemID, name string) error
	SetItemOwner(ctx context.Context, id model.ItemID, owner model.UserID) error
	ItemHistory(ctx context.Context, id model.ItemID) ([]*model.Event, error)
	SearchItem(ctx context.Context, query string, limit int) ([]*model.Item, error)
	ValidateItem(ctx context.Context, e *model.Item) error
}
