package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// --- AT Proto identity ---

// ConnectATProto stores a user's AT Proto DID, PDS URL, and session tokens.
// This is called after the user authenticates with their PDS.
func (s *Store) ConnectATProto(ctx context.Context, userID int, did, pdsURL, accessToken, refreshToken string, expiresAt *time.Time) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE users
		SET did                      = $2,
		    pds_url                  = $3,
		    atproto_access_token     = $4,
		    atproto_refresh_token    = $5,
		    atproto_token_expires_at = $6
		WHERE id = $1`,
		userID, did, pdsURL, accessToken, refreshToken, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("connect atproto: %w", err)
	}
	return nil
}

// DisconnectATProto clears the PDS session credentials for a user. The DID
// (the user's permanent identity) is retained.
func (s *Store) DisconnectATProto(ctx context.Context, userID int) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE users
		SET pds_url                  = NULL,
		    atproto_access_token     = NULL,
		    atproto_refresh_token    = NULL,
		    atproto_token_expires_at = NULL,
		    oauth_session_id         = NULL
		WHERE id = $1`, userID,
	)
	return err
}

// GetATProtoCredentials returns the PDS credentials for a user, or nil if not connected.
func (s *Store) GetATProtoCredentials(ctx context.Context, userID int) (*ATProtoCredentials, error) {
	var c ATProtoCredentials
	var pdsURL, access, refresh sql.NullString
	var expiresAt sql.NullTime
	err := s.DB.QueryRowContext(ctx, `
		SELECT id, did, pds_url,
		       COALESCE(atproto_access_token,''),
		       COALESCE(atproto_refresh_token,''),
		       atproto_token_expires_at
		FROM users WHERE id = $1`, userID,
	).Scan(&c.UserID, &c.DID, &pdsURL, &access, &refresh, &expiresAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get atproto credentials: %w", err)
	}
	c.PDSUrl = pdsURL.String
	c.AccessToken = access.String
	c.RefreshToken = refresh.String
	if expiresAt.Valid {
		t := expiresAt.Time
		c.ExpiresAt = &t
	}
	if !pdsURL.Valid {
		return nil, nil // not connected to a PDS
	}
	return &c, nil
}

// SetUserPDSSession records the user's PDS host and the OAuth session ID that
// the API should resume to write io.sunred.* records to their PDS. Called from
// the OAuth callback. The session data itself is persisted in oauth_sessions
// by the indigo OAuth client; this column identifies the active session.
func (s *Store) SetUserPDSSession(ctx context.Context, userID int, pdsURL, sessionID string) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE users SET pds_url = $2, oauth_session_id = $3 WHERE id = $1`,
		userID, pdsURL, sessionID,
	)
	return err
}

// GetUserOAuthSession returns the DID, OAuth session ID, and PDS URL for a
// user. sessionID is "" when the user has no persisted OAuth session (e.g. a
// remote user never logged in here). did is "" when the user has no AT Proto
// identity.
func (s *Store) GetUserOAuthSession(ctx context.Context, userID int) (did, sessionID, pdsURL string, err error) {
	var d, sess, p sql.NullString
	err = s.DB.QueryRowContext(ctx, `
		SELECT did, oauth_session_id, pds_url FROM users WHERE id = $1`, userID,
	).Scan(&d, &sess, &p)
	if err == sql.ErrNoRows {
		return "", "", "", nil
	}
	return d.String, sess.String, p.String, err
}

// UpdateATProtoTokens updates the access/refresh tokens for a user after a refresh.
func (s *Store) UpdateATProtoTokens(ctx context.Context, userID int, accessToken, refreshToken string, expiresAt *time.Time) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE users
		SET atproto_access_token     = $2,
		    atproto_refresh_token    = $3,
		    atproto_token_expires_at  = $4
		WHERE id = $1`,
		userID, accessToken, refreshToken, expiresAt,
	)
	return err
}

// ListUsersWithATProto returns all users that have a connected AT Proto identity.
// Used by the poller to know which users to sync.
func (s *Store) ListUsersWithATProto(ctx context.Context) ([]ATProtoCredentials, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id,
		       COALESCE(did,''), COALESCE(pds_url,''),
		       COALESCE(atproto_access_token,''),
		       COALESCE(atproto_refresh_token,''),
		       atproto_token_expires_at
		FROM users
		WHERE pds_url IS NOT NULL`,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []ATProtoCredentials
	for rows.Next() {
		var c ATProtoCredentials
		var expiresAt sql.NullTime
		if err := rows.Scan(
			&c.UserID, &c.DID, &c.PDSUrl,
			&c.AccessToken, &c.RefreshToken, &expiresAt,
		); err != nil {
			return nil, err
		}
		if expiresAt.Valid {
			t := expiresAt.Time
			c.ExpiresAt = &t
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// --- AT Proto rkey tracking ---

// SetFollowATProtoRkey records the AT Proto rkey for a follow row.
func (s *Store) SetFollowATProtoRkey(ctx context.Context, followerID, followeeID int, rkey string) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE user_follows SET atproto_rkey = $3
		WHERE follower_id = $1 AND followee_id = $2`,
		followerID, followeeID, rkey,
	)
	return err
}

// GetFollowATProtoRkey retrieves the AT Proto rkey for a follow relationship.
func (s *Store) GetFollowATProtoRkey(ctx context.Context, followerID, followeeID int) (string, error) {
	var rkey sql.NullString
	err := s.DB.QueryRowContext(ctx, `
		SELECT atproto_rkey FROM user_follows
		WHERE follower_id = $1 AND followee_id = $2`,
		followerID, followeeID,
	).Scan(&rkey)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return rkey.String, err
}

// SetShareATProtoRkey records the AT Proto rkey for a shared article row.
func (s *Store) SetShareATProtoRkey(ctx context.Context, shareID int64, rkey string) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE shared_articles SET atproto_rkey = $2 WHERE id = $1`, shareID, rkey,
	)
	return err
}

// GetShareATProtoRkey retrieves the AT Proto rkey for a share row.
func (s *Store) GetShareATProtoRkey(ctx context.Context, shareID int64) (string, error) {
	var rkey sql.NullString
	err := s.DB.QueryRowContext(ctx, `
		SELECT atproto_rkey FROM shared_articles WHERE id = $1`, shareID,
	).Scan(&rkey)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return rkey.String, err
}

// SetFeedATProtoRkey records the AT Proto rkey for a user's subscription row.
func (s *Store) SetFeedATProtoRkey(ctx context.Context, userID, feedID int, rkey string) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE subscriptions SET atproto_rkey = $3, updated_at = NOW()
		 WHERE user_id = $1 AND feed_id = $2`, userID, feedID, rkey,
	)
	return err
}

// GetFeedATProtoRkey retrieves the AT Proto rkey for a user's subscription.
func (s *Store) GetFeedATProtoRkey(ctx context.Context, userID, feedID int) (string, error) {
	var rkey sql.NullString
	err := s.DB.QueryRowContext(ctx, `
		SELECT atproto_rkey FROM subscriptions WHERE user_id = $1 AND feed_id = $2`, userID, feedID,
	).Scan(&rkey)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return rkey.String, err
}

// --- Relay cursor ---

// GetRelayCursor returns the current relay WebSocket resume cursor.
func (s *Store) GetRelayCursor(ctx context.Context) (string, int64, error) {
	var relayURL string
	var seq int64
	err := s.DB.QueryRowContext(ctx, `
		SELECT relay_url, cursor_seq FROM atproto_relay_cursor WHERE id = 1`,
	).Scan(&relayURL, &seq)
	if err == sql.ErrNoRows {
		return "", 0, nil
	}
	return relayURL, seq, err
}

// UpdateRelayCursor persists the latest relay event sequence number.
func (s *Store) UpdateRelayCursor(ctx context.Context, relayURL string, seq int64) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE atproto_relay_cursor
		SET relay_url = $1, cursor_seq = $2, updated_at = NOW()
		WHERE id = 1`,
		relayURL, seq,
	)
	return err
}
