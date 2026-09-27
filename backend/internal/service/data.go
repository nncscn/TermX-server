package service

// TODO: 导出导入客户端化还没做
import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"ky/internal/model"
	"ky/internal/pkg/vault"
	"ky/internal/repository"
)

// ExportKeyItem 导出的密钥条目：元信息 + 解密后的密钥内容。
type ExportKeyItem struct {
	model.SshKey
	PublicKey  string `json:"public_key"`
	PrivateKey string `json:"private_key"`
	Passphrase string `json:"passphrase"`
}

// ExportCredItem 导出的凭据条目：元信息 + 解密后的连接信息。
type ExportCredItem struct {
	model.Credential
	Host       string      `json:"host"`
	Port       *int        `json:"port"`
	Username   string      `json:"username"`
	Password   string      `json:"password"`
	KeyContent string      `json:"key_content"`
	Proxy      ProxySecret `json:"proxy"`
	Serial     *SerialConf `json:"serial"`
}

// ImportKeyItem 导入的密钥条目。ID 仅作导入包内部引用锚点（供凭据 key_id 重映射），
type ImportKeyItem struct {
	ID          uint       `json:"id"`
	Name        string     `json:"name" binding:"required,min=2,max=40"`
	Type        string     `json:"type" binding:"required,oneof=ed25519 rsa2048 rsa4096 ecdsa"`
	Fingerprint string     `json:"fingerprint"`
	PublicKey   string     `json:"public_key"`
	PrivateKey  string     `json:"private_key"`
	Passphrase  string     `json:"passphrase"`
	Remark      string     `json:"remark" binding:"max=120"`
	CreatedAt   *time.Time `json:"created_at"`
	UpdatedAt   *time.Time `json:"updated_at"`
	DeletedAt   *time.Time `json:"deleted_at"`
}

// ImportCredItem 导入的凭据条目。KeyID 引用导入包中密钥条目的 ID，导入时重映射为新主键
type ImportCredItem struct {
	Name       string      `json:"name" binding:"required,min=2,max=40"`
	Protocol   string      `json:"protocol" binding:"required,oneof=ssh rdp vnc telnet serial"`
	AuthType   string      `json:"auth_type" binding:"required,oneof=password key"`
	Group      string      `json:"group" binding:"max=40"`
	KeyID      *uint       `json:"key_id"`
	Host       string      `json:"host" binding:"required,max=255"`
	Port       *int        `json:"port"`
	Username   string      `json:"username" binding:"max=64"`
	Password   string      `json:"password" binding:"max=128"`
	KeyContent string      `json:"key_content" binding:"max=16384"`
	Proxy      ProxySecret `json:"proxy"`
	Serial     *SerialConf `json:"serial"`
	Remark     string      `json:"remark" binding:"max=120"`
	LastUsedAt *time.Time  `json:"last_used_at"`
	CreatedAt  *time.Time  `json:"created_at"`
	UpdatedAt  *time.Time  `json:"updated_at"`
	DeletedAt  *time.Time  `json:"deleted_at"`
}

type DataService struct {
	keys  *repository.SshKeyRepository
	creds *repository.CredentialRepository
	auth  *AuthService
}

func NewDataService(keys *repository.SshKeyRepository, creds *repository.CredentialRepository, auth *AuthService) *DataService {
	return &DataService{keys: keys, creds: creds, auth: auth}
}

func (s *DataService) ExportBackup(ctx context.Context, accountID uint, password string) ([]ExportKeyItem, []ExportCredItem, error) {
	vk, err := s.auth.VerifyPassword(ctx, accountID, password)
	if err != nil {
		return nil, nil, err
	}
	keys, _, err := s.keys.List(ctx, accountID, repository.KeyFilter{IncludeDeleted: true})
	if err != nil {
		return nil, nil, fmt.Errorf("查询密钥: %w", err)
	}
	creds, _, err := s.creds.List(ctx, accountID, repository.CredFilter{IncludeDeleted: true})
	if err != nil {
		return nil, nil, fmt.Errorf("查询凭据: %w", err)
	}

	keyItems := make([]ExportKeyItem, 0, len(keys))
	for _, k := range keys {
		sec := KeySecret{}
		if raw, err := vault.Open(vk, k.Secret); err == nil {
			_ = json.Unmarshal(raw, &sec)
		}
		keyItems = append(keyItems, ExportKeyItem{
			SshKey: k, PublicKey: sec.PublicKey, PrivateKey: sec.PrivateKey, Passphrase: sec.Passphrase,
		})
	}
	credItems := make([]ExportCredItem, 0, len(creds))
	for _, c := range creds {
		sec := CredSecret{}
		if raw, err := vault.Open(vk, c.Secret); err == nil {
			_ = json.Unmarshal(raw, &sec)
		}
		credItems = append(credItems, ExportCredItem{
			Credential: c, Host: sec.Host, Port: sec.Port, Username: sec.Username,
			Password: sec.Password, KeyContent: sec.KeyContent, Proxy: sec.Proxy, Serial: sec.Serial,
		})
	}
	return keyItems, credItems, nil
}

// ImportBackup 验主密码后用导入包整体替换当前账户数据（先清空再重灌,服务端加密）
func (s *DataService) ImportBackup(ctx context.Context, accountID uint, password string, keys []ImportKeyItem, creds []ImportCredItem) (int, int, error) {
	vk, err := s.auth.VerifyPassword(ctx, accountID, password)
	if err != nil {
		return 0, 0, err
	}
	return s.replaceAll(ctx, accountID, vk, keys, creds)
}

// replaceAll 整体替换：清空两表 → 重灌密钥（记录旧 ID → 新 ID 映射）→ 重灌凭据
// （key_id 按映射重写，引用缺失时置空降级为密码认证数据形态）
func (s *DataService) replaceAll(ctx context.Context, accountID uint, vk []byte, keys []ImportKeyItem, creds []ImportCredItem) (int, int, error) {
	if err := s.keys.DeleteAll(ctx, accountID); err != nil {
		return 0, 0, fmt.Errorf("清空密钥: %w", err)
	}
	if err := s.creds.DeleteAll(ctx, accountID); err != nil {
		return 0, 0, fmt.Errorf("清空凭据: %w", err)
	}

	idMap := make(map[uint]uint, len(keys))
	for _, in := range keys {
		sec := KeySecret{PublicKey: in.PublicKey, PrivateKey: in.PrivateKey, Passphrase: in.Passphrase}
		blob, err := vault.Seal(vk, mustJSON(sec))
		if err != nil {
			return 0, 0, err
		}
		k := &model.SshKey{
			AccountID: accountID, Name: in.Name, Type: in.Type,
			Fingerprint: in.Fingerprint, // 指纹由客户端计算提交（服务端仅见密文）
			Secret:      blob, Remark: in.Remark,
			CreatedAt: orNow(in.CreatedAt), UpdatedAt: orNow(in.UpdatedAt), DeletedAt: in.DeletedAt,
		}
		if err := s.keys.Create(ctx, k); err != nil {
			return 0, 0, fmt.Errorf("导入密钥: %w", err)
		}
		if in.ID != 0 {
			idMap[in.ID] = k.ID
		}
	}

	for _, in := range creds {
		sec := CredSecret{
			Host: in.Host, Port: in.Port, Username: in.Username, Password: in.Password,
			KeyContent: in.KeyContent, Proxy: in.Proxy, Serial: in.Serial,
		}
		blob, err := vault.Seal(vk, mustJSON(sec))
		if err != nil {
			return 0, 0, err
		}
		var keyID *uint
		if in.KeyID != nil && *in.KeyID != 0 {
			if nid, ok := idMap[*in.KeyID]; ok {
				keyID = &nid
			}
		}
		c := &model.Credential{
			AccountID: accountID, Name: in.Name, Protocol: in.Protocol, AuthType: in.AuthType,
			Group: in.Group, KeyID: keyID, Secret: blob, Remark: in.Remark,
			LastUsedAt: in.LastUsedAt, CreatedAt: orNow(in.CreatedAt),
			UpdatedAt: orNow(in.UpdatedAt), DeletedAt: in.DeletedAt,
		}
		if err := s.creds.Create(ctx, c); err != nil {
			return 0, 0, fmt.Errorf("导入凭据: %w", err)
		}
	}
	return len(keys), len(creds), nil
}

func mustJSON(v any) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("序列化导入内容: %v", err))
	}
	return raw
}

func orNow(t *time.Time) time.Time {
	if t != nil {
		return *t
	}
	return time.Now()
}
