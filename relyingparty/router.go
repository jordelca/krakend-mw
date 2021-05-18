package relyingparty

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/devopsfaith/krakend/config"
	"github.com/devopsfaith/krakend/proxy"
	krakendgin "github.com/devopsfaith/krakend/router/gin"
	"github.com/dgrijalva/jwt-go"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

const (
	namespace           = "github.com/jordelca/krakend-mw/relyingparty"
	HeaderAuthorization = "Authorization"
	HeaderUserID        = "User-Id"
	TokenType           = "Bearer"
)

type EndpointMw func(gin.HandlerFunc) gin.HandlerFunc

// NewHandlerFactory builds a oauth2 wrapper over the received handler factory.
// Run for each endpoints.
func NewHandlerFactory(next krakendgin.HandlerFactory, rp *RelyingParty) krakendgin.HandlerFactory {
	return func(remote *config.EndpointConfig, p proxy.Proxy) gin.HandlerFunc {
		var err error
		handlerFunc := next(remote, p)
		eCfg, ok := remote.ExtraConfig[namespace]
		if !ok {
			return handlerFunc
		}
		cfg, err := getEpConfig(eCfg)
		if err != nil {
			logrus.WithError(err).Errorln("getEpConfig error")
			return handlerFunc
		}
		return newEndpointRelyingPartyMw(cfg, rp)(handlerFunc)
	}
}

// newEndpointRelyingPartyMw is the handler middlware that implements token-based auth.
func newEndpointRelyingPartyMw(cfg *epConfig, rp *RelyingParty) EndpointMw {
	return func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			userToken := c.GetHeader(HeaderAuthorization)
			if len(userToken) == 0 {
				logrus.Warnln("empty user token")
				c.AbortWithStatusJSON(http.StatusUnauthorized, newErr(invalidToken, "token not exists"))
				return
			}
			items := strings.Split(userToken, " ")
			if len(items) != 2 || items[0] != TokenType {
				logrus.Warnln("invalid token")
				c.AbortWithStatusJSON(http.StatusUnauthorized, newErr(invalidToken, "token is malformed"))
				return
			}

			token, err := jwt.Parse(items[1], func(token *jwt.Token) (interface{}, error) {
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
				}
				return []byte(rp.cfg.TokenSecret), nil
			})
			if err != nil {
				logrus.WithError(err).Warnln("parse token err")
				c.AbortWithStatusJSON(http.StatusUnauthorized, newErr(invalidToken, err.Error()))
				return
			}

			if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
				userID, ok := claims["user_id"].(string)
				if !ok {
					logrus.Warnln("user id in claims not exist")
					c.AbortWithStatusJSON(http.StatusUnauthorized, invalidUserIDErr)
					return
				}
				userRole, ok := claims["user_role"].(string)
				if !ok {
					logrus.Warnln("user role in claims not exist")
					c.AbortWithStatusJSON(http.StatusUnauthorized, invalidUserRoleErr)
					return
				}
				if !matchRoles(userRole, cfg.Roles) {
					logrus.WithField("role", userRole).Warnln("access denied")
					c.AbortWithStatusJSON(http.StatusForbidden, accessDenied)
					return
				}
				c.Request.Header.Set(HeaderUserID, userID)
			} else {
				logrus.WithError(err).Warnln("claims err")
				c.AbortWithStatusJSON(
					http.StatusUnauthorized,
					newErr(
						invalidTokenClaims,
						err.Error(),
					),
				)
				return
			}
			next(c)
		}
	}
}

// matchRoles looking for matching roles
func matchRoles(userRole string, acceptRoles []string) bool {
	for _, r := range acceptRoles {
		if r == userRole {
			return true
		}
	}
	return false
}
