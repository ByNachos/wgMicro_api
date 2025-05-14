// internal/auth/auth.go
package auth

import (
	"os"
	"time"

	jwt "github.com/appleboy/gin-jwt/v2"
	"github.com/gin-gonic/gin"
)

// Ключ для подписи JWT – храните его в .env или в безопасном хранилище
var jwtSecret = []byte(os.Getenv("JWT_SECRET"))

// IdentityKey – имя поля в claims
const IdentityKey = "id"

// User – структура, возвращаемая из PayloadFunc
type User struct {
	UserName string
}

// PayloadFunc создаёт поля токена
func payloadFunc(data interface{}) jwt.MapClaims {
	if v, ok := data.(*User); ok {
		return jwt.MapClaims{
			IdentityKey: v.UserName,
		}
	}
	return jwt.MapClaims{}
}

// Authenticator проверяет логин/пароль и возвращает User
func authenticator(c *gin.Context) (interface{}, error) {
	var loginVals struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&loginVals); err != nil {
		return "", jwt.ErrMissingLoginValues
	}
	// FIXME: замените жёсткую проверку на запрос к БД
	if loginVals.Username == "admin" && loginVals.Password == "admin" {
		return &User{UserName: "admin"}, nil
	}
	return nil, jwt.ErrFailedAuthentication
}

// Authorizator – разрешает доступ только админу
func authorizator(data interface{}, c *gin.Context) bool {
	if v, ok := data.(*User); ok && v.UserName == "admin" {
		return true
	}
	return false
}

// JWTMiddleware инициализирует middleware для Gin
func JWTMiddleware() (*jwt.GinJWTMiddleware, error) {
	return jwt.New(&jwt.GinJWTMiddleware{
		Realm:           "wg-api",
		Key:             jwtSecret,
		Timeout:         time.Hour,
		MaxRefresh:      time.Hour,
		IdentityKey:     IdentityKey,
		PayloadFunc:     payloadFunc,
		IdentityHandler: func(c *gin.Context) interface{} { return &User{UserName: c.GetString(IdentityKey)} },
		Authenticator:   authenticator,
		Authorizator:    authorizator,
		Unauthorized: func(c *gin.Context, code int, message string) {
			c.JSON(code, gin.H{"error": message})
		},
		TokenLookup:   "header: Authorization, query: token",
		TokenHeadName: "Bearer",
		TimeFunc:      time.Now,
	})
}
