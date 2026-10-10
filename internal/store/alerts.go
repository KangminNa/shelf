package store

import (
	"context"
	"time"

	"github.com/KangminNa/naru/internal/model"
)

// Channels는 알림 주소를 저장한다 (ChannelStore). 주소와 시크릿은 비밀이다.
type Channels struct{ db *DB }

func NewChannels(db *DB) Channels { return Channels{db} }

func (c Channels) List(ctx context.Context) ([]model.AlertChannel, error) {
	rows, err := c.db.sql.QueryContext(ctx, `SELECT id, name, url, secret FROM alert_channels ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.AlertChannel
	for rows.Next() {
		var a model.AlertChannel
		var id int64
		if err := rows.Scan(&id, &a.Name, &a.URL, &a.Secret); err != nil {
			return nil, err
		}
		a.ID = model.ChannelID(id)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (c Channels) Add(ctx context.Context, a model.AlertChannel) (model.ChannelID, error) {
	res, err := c.db.sql.ExecContext(ctx, `INSERT INTO alert_channels (name, url, secret) VALUES (?, ?, ?)`, a.Name, a.URL, a.Secret)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	return model.ChannelID(id), err
}

func (c Channels) Remove(ctx context.Context, id model.ChannelID) error {
	res, err := c.db.sql.ExecContext(ctx, `DELETE FROM alert_channels WHERE id = ?`, int64(id))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return model.ErrNotFound
	}
	return nil
}

// keepDeliveries만큼 최근 결과를 남긴다.
const keepDeliveries = 200

// Deliveries는 보낸 결과를 남긴다 (DeliveryLog).
type Deliveries struct{ db *DB }

func NewDeliveries(db *DB) Deliveries { return Deliveries{db} }

func (d Deliveries) Save(ctx context.Context, x model.Delivery) error {
	_, err := d.db.sql.ExecContext(ctx, `INSERT INTO alert_deliveries (channel_id, channel_name, event, title, ok, detail, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		int64(x.Channel), x.ChannelName, x.Event, x.Title, x.OK, x.Detail, x.At.Unix())
	if err != nil {
		return err
	}
	_, err = d.db.sql.ExecContext(ctx, `DELETE FROM alert_deliveries WHERE id NOT IN (SELECT id FROM alert_deliveries ORDER BY id DESC LIMIT ?)`, keepDeliveries)
	return err
}

func (d Deliveries) Recent(ctx context.Context, n int) ([]model.Delivery, error) {
	rows, err := d.db.sql.QueryContext(ctx, `SELECT channel_id, channel_name, event, title, ok, detail, created_at FROM alert_deliveries ORDER BY id DESC LIMIT ?`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Delivery
	for rows.Next() {
		var x model.Delivery
		var ch, at int64
		if err := rows.Scan(&ch, &x.ChannelName, &x.Event, &x.Title, &x.OK, &x.Detail, &at); err != nil {
			return nil, err
		}
		x.Channel, x.At = model.ChannelID(ch), time.Unix(at, 0)
		out = append(out, x)
	}
	return out, rows.Err()
}
