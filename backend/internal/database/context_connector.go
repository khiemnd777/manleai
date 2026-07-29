package database

import (
	"context"
	"database/sql/driver"
	"errors"

	"github.com/lib/pq"
	"github.com/manleai/ai-receptionist/internal/databasecontext"
)

type contextConnector struct{ base driver.Connector }

func newContextConnector(databaseURL string) (driver.Connector, error) {
	base, err := pq.NewConnector(databaseURL)
	if err != nil {
		return nil, err
	}
	return &contextConnector{base: base}, nil
}

func (c *contextConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.base.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &contextConn{Conn: conn}, nil
}

func (c *contextConnector) Driver() driver.Driver { return c.base.Driver() }

type contextConn struct{ driver.Conn }

func (c *contextConn) Prepare(query string) (driver.Stmt, error) {
	return c.Conn.Prepare(query)
}

func (c *contextConn) Begin() (driver.Tx, error) { return c.Conn.Begin() }

func (c *contextConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if beginner, ok := c.Conn.(driver.ConnBeginTx); ok {
		return beginner.BeginTx(ctx, opts)
	}
	return c.Conn.Begin()
}

func (c *contextConn) Ping(ctx context.Context) error {
	if pinger, ok := c.Conn.(driver.Pinger); ok {
		return pinger.Ping(ctx)
	}
	return nil
}

func (c *contextConn) ResetSession(ctx context.Context) error {
	if err := c.resetAccessContext(ctx); err != nil {
		return driver.ErrBadConn
	}
	if resetter, ok := c.Conn.(driver.SessionResetter); ok {
		return resetter.ResetSession(ctx)
	}
	return nil
}

func (c *contextConn) CheckNamedValue(value *driver.NamedValue) error {
	if checker, ok := c.Conn.(driver.NamedValueChecker); ok {
		return checker.CheckNamedValue(value)
	}
	return driver.ErrSkip
}

func (c *contextConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	execer, ok := c.Conn.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	if err := c.applyAccessContext(ctx); err != nil {
		return nil, err
	}
	result, queryErr := execer.ExecContext(ctx, query, args)
	resetErr := c.resetAccessContext(context.Background())
	return result, errors.Join(queryErr, resetErr)
}

func (c *contextConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	queryer, ok := c.Conn.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	if err := c.applyAccessContext(ctx); err != nil {
		return nil, err
	}
	rows, err := queryer.QueryContext(ctx, query, args)
	if err != nil {
		return nil, errors.Join(err, c.resetAccessContext(context.Background()))
	}
	return &contextRows{Rows: rows, reset: func() error {
		return c.resetAccessContext(context.Background())
	}}, nil
}

func (c *contextConn) applyAccessContext(ctx context.Context) error {
	access := databasecontext.FromContext(ctx)
	return c.setAccessContext(ctx, access.ActorUserID, access.Scope, access.SystemSalonID)
}

func (c *contextConn) resetAccessContext(ctx context.Context) error {
	return c.setAccessContext(ctx, "", "", "")
}

func (c *contextConn) setAccessContext(ctx context.Context, actorUserID, scope, systemSalonID string) error {
	execer, ok := c.Conn.(driver.ExecerContext)
	if !ok {
		return driver.ErrBadConn
	}
	_, err := execer.ExecContext(ctx, `SELECT set_config('app.actor_user_id',$1,false), set_config('app.database_scope',$2,false), set_config('app.system_salon_id',$3,false)`, []driver.NamedValue{
		{Ordinal: 1, Value: actorUserID},
		{Ordinal: 2, Value: scope},
		{Ordinal: 3, Value: systemSalonID},
	})
	return err
}

type contextRows struct {
	driver.Rows
	reset func() error
}

func (r *contextRows) Close() error {
	closeErr := r.Rows.Close()
	resetErr := r.reset()
	return errors.Join(closeErr, resetErr)
}
