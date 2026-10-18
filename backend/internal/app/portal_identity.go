package app

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
	"xhs-workbench/internal/store"
)

type Account struct {
	ID                 string `json:"id"`
	Username           string `json:"username"`
	DisplayName        string `json:"display_name"`
	Role               string `json:"role"`
	Enabled            bool   `json:"enabled"`
	MustChangePassword bool   `json:"must_change_password"`
	CreatedAt          string `json:"created_at"`
	LastLogin          string `json:"last_login,omitempty"`
	DailyRequestLimit  int    `json:"daily_request_limit"`
	MaxConcurrent      int    `json:"max_concurrent"`
}
type accountRecord struct {
	Account
	PasswordHash string `json:"password_hash"`
	Version      int    `json:"version"`
	Primary      bool   `json:"primary,omitempty"`
}
type identityMeta struct {
	PrimaryUserID string `json:"primary_user_id"`
	Bootstrap     string `json:"bootstrap"`
}
type portalSession struct {
	Username  string `json:"username,omitempty"`
	Realm     string `json:"realm"`
	Version   int    `json:"version"`
	CSRF      string `json:"csrf"`
	ExpiresAt int64  `json:"expires_at"`
	LastSeen  int64  `json:"last_seen"`
}
type AuditEvent struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
	ActorID   string `json:"actor_id,omitempty"`
	ActorName string `json:"actor_name"`
	Action    string `json:"action"`
	Target    string `json:"target,omitempty"`
	Result    string `json:"result"`
}
type loginAttempts struct {
	Count int
	Until time.Time
}
type quotaCounter struct {
	Requests int `json:"requests"`
}

type Portal struct {
	root         *App
	identity     *store.Store
	mu           sync.Mutex
	runtimes     map[string]*App
	handlers     map[string]portalHandler
	attempts     map[string]loginAttempts
	inflight     map[string]int
	meta         identityMeta
	dummyHash    []byte
	startWorkers bool
	closed       bool
}

var accountName = regexp.MustCompile(`^[a-z][a-z0-9_-]{2,31}$`)

func randomSecret() string {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}
func sessionHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
func validPassword(password string) bool {
	return utf8.RuneCountInString(password) >= 12 && len(password) <= 72
}

func NewPortal(root *App) (*Portal, error) {
	identity, err := store.Open(filepath.Join(root.Data, "identity.db"))
	if err != nil {
		return nil, err
	}
	p := &Portal{root: root, identity: identity, runtimes: map[string]*App{}, handlers: map[string]portalHandler{}, attempts: map[string]loginAttempts{}, inflight: map[string]int{}}
	p.dummyHash, _ = bcrypt.GenerateFromPassword([]byte(randomSecret()), bcrypt.DefaultCost)
	if err = p.bootstrap(); err != nil {
		identity.Close()
		return nil, err
	}
	return p, nil
}

func (p *Portal) bootstrap() error {
	err := p.identity.Get("identity_meta", "main", &p.meta)
	if errors.Is(err, store.ErrNotFound) {
		var delivery strings.Builder
		delivery.WriteString("RedNote_BuddY 登录凭据\n仅保存在本机 data 目录，不应上传到仓库。首次登录必须修改密码。\n\n")
		accounts := []accountRecord{}
		for _, role := range []string{"admin", "user"} {
			password := randomSecret()
			hash, e := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
			if e != nil {
				return e
			}
			name, label, entry := "user", "创作用户", "http://127.0.0.1:8080/"
			if role == "admin" {
				name, label, entry = "admin", "系统管理员", "http://127.0.0.1:8080/admin"
			}
			actualRole := "member"
			if role == "admin" {
				actualRole = "admin"
			}
			account := accountRecord{Account: Account{ID: ID(), Username: name, DisplayName: label, Role: actualRole, Enabled: true, MustChangePassword: true, CreatedAt: now(), MaxConcurrent: 2}, PasswordHash: string(hash), Version: 1, Primary: role == "user"}
			if account.Primary {
				p.meta.PrimaryUserID = account.ID
			}
			accounts = append(accounts, account)
			fmt.Fprintf(&delivery, "%s\n入口：%s\n账号：%s\n初始密码：%s\n\n", label, entry, name, password)
		}
		p.meta.Bootstrap, err = cryptSecret(delivery.String(), true)
		if err != nil {
			return err
		}
		err = p.identity.Atomic(func(tx *store.Transaction) error {
			for _, a := range accounts {
				if e := tx.Put("accounts", a.Username, a); e != nil {
					return e
				}
			}
			return tx.Put("identity_meta", "main", p.meta)
		})
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	accounts, err := p.accounts()
	if err != nil {
		return err
	}
	needDelivery := false
	for _, a := range accounts {
		if (a.Username == "user" || a.Username == "admin") && a.MustChangePassword {
			needDelivery = true
		}
	}
	if needDelivery {
		file := filepath.Join(p.root.Data, "initial-accounts.txt")
		if _, e := os.Stat(file); errors.Is(e, os.ErrNotExist) {
			text, e := cryptSecret(p.meta.Bootstrap, false)
			if e != nil {
				return e
			}
			if e = os.WriteFile(file, []byte("\ufeff"+text), 0600); e != nil {
				return e
			}
		}
	}
	for _, a := range accounts {
		if a.Primary {
			if a.ID != p.meta.PrimaryUserID || a.Role != "member" {
				return errors.New("invalid legacy owner mapping")
			}
			p.runtimes[a.ID] = p.root
			p.configureRuntime(p.root, a)
		}
	}
	if p.runtimes[p.meta.PrimaryUserID] == nil {
		return errors.New("legacy data owner missing")
	}
	return nil
}

func (p *Portal) accounts() ([]accountRecord, error) {
	rows, err := p.identity.List("accounts")
	if err != nil {
		return nil, err
	}
	return store.Decode[accountRecord](rows)
}
func (p *Portal) accountByID(id string) (accountRecord, error) {
	accounts, err := p.accounts()
	if err != nil {
		return accountRecord{}, err
	}
	for _, a := range accounts {
		if a.ID == id {
			return a, nil
		}
	}
	return accountRecord{}, store.ErrNotFound
}
func (p *Portal) audit(actor Account, action, target, result string) {
	event := AuditEvent{ID: ID(), CreatedAt: now(), ActorID: actor.ID, ActorName: actor.Username, Action: action, Target: target, Result: result}
	_ = p.identity.Put("audit", event.ID, event)
}

func (p *Portal) Close() error {
	p.mu.Lock()
	p.closed = true
	apps := []*App{}
	for _, a := range p.runtimes {
		if a != p.root {
			apps = append(apps, a)
		}
	}
	p.mu.Unlock()
	p.root.cancel()
	p.root.wg.Wait()
	for _, a := range apps {
		_ = a.Close()
	}
	return p.identity.Close()
}
