/*
Copyright (c) 2014 Ashley Jeffs

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, sub to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/

package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/garyburd/redigo/redis"
	"github.com/jeffail/leaps/lib/register"
	"github.com/jeffail/util/log"
)

/*--------------------------------------------------------------------------------------------------
 */

/*
RedisConfig - A config object for the redis authentication object.
*/
type RedisConfig struct {
	URL          string `json:"url" yaml:"url"`
	Password     string `json:"password" yaml:"password"`
	PoolIdleTOut int64  `json:"pool_idle_s" yaml:"pool_idle_s"`
	PoolMaxIdle  int    `json:"pool_max_idle" yaml:"pool_max_idle"`
}

/*
NewRedisConfig - Returns a default config object for a Redis.
*/
func NewRedisConfig() RedisConfig {
	return RedisConfig{
		URL:          ":6379",
		Password:     "",
		PoolIdleTOut: 240,
		PoolMaxIdle:  3,
	}
}

/*--------------------------------------------------------------------------------------------------
 */

func newPool(config RedisConfig) *redis.Pool {
	return &redis.Pool{
		MaxIdle:     config.PoolMaxIdle,
		IdleTimeout: time.Duration(config.PoolIdleTOut) * time.Second,
		Dial: func() (redis.Conn, error) {
			c, err := redis.Dial("tcp", config.URL)
			if err != nil {
				return nil, err
			}
			if 0 != len(config.Password) {
				if _, err := c.Do("AUTH", config.Password); err != nil {
					c.Close()
					return nil, err
				}
			}
			return c, err
		},
		TestOnBorrow: func(c redis.Conn, t time.Time) error {
			_, err := c.Do("PING")
			return err
		},
	}
}

/*--------------------------------------------------------------------------------------------------
 */

// Errors for the Redis type.
var (
	ErrNoKey = errors.New("key did not exist")
)

/*
Redis - A wrapper around the Redis client that acts as an authenticator.
*/
type Redis struct {
	logger *log.Logger
	config Config
	pool   *redis.Pool
}

/*
NewRedis - Creates a Redis using the provided configuration.
*/
func NewRedis(config Config, logger *log.Logger) *Redis {
	return &Redis{
		logger: logger.NewModule(":redis_auth"),
		config: config,
		pool:   newPool(config.RedisConfig),
	}
}

/*--------------------------------------------------------------------------------------------------
 */

/*
AuthoriseCreate - Checks whether a specific key exists in Redis and that the value matches our user
ID.
*/
func (s *Redis) AuthoriseCreate(token, userID string) bool {
	if !s.config.AllowCreate {
		return false
	}
	userKey, err := s.ReadKey(token)
	if err != nil {
		s.logger.Errorf("failed to get authorise create token: %v\n", err)
		return false
	}
	if userKey != userID {
		s.logger.Warnf("create token invalid, provided: %v, actual: %v\n", userID, userKey)
		return false
	}
	err = s.DeleteKey(token)
	if err != nil {
		s.logger.Errorf("failed to delete key: %v\n", token)
	}
	return true
}

/*
AuthoriseJoin - Checks whether a specific key exists in Redis and that the value matches a document
ID.
*/
func (s *Redis) AuthoriseJoin(token, documentID string) bool {
	docKey, err := s.ReadKey(token)
	if err != nil {
		s.logger.Errorf("failed to get authorise join token: %v\n", err)
		return false
	}
	if docKey != documentID {
		s.logger.Warnf("join token invalid, provided: %v, actual: %v\n", documentID, docKey)
		return false
	}
	err = s.DeleteKey(token)
	if err != nil {
		s.logger.Errorf("failed to delete key: %v\n", token)
	}
	return true
}

/*
AuthoriseReadOnly - Checks whether a specific key exists in Redis and that the value matches a
document ID.
*/
func (s *Redis) AuthoriseReadOnly(token, documentID string) bool {
	docKey, err := s.ReadKey(token)
	if err != nil {
		s.logger.Errorf("failed to get authorise join token: %v\n", err)
		return false
	}
	expectedKey := fmt.Sprintf("%v:%v", "READ-ONLY", documentID)
	if docKey != expectedKey {
		s.logger.Warnf("join token invalid, provided: %v, actual: %v\n", expectedKey, docKey)
		return false
	}
	err = s.DeleteKey(token)
	if err != nil {
		s.logger.Errorf("failed to delete key: %v\n", token)
	}
	return true
}

/*
RegisterHandlers - Nothing to register.
*/
func (s *Redis) RegisterHandlers(register.PubPrivEndpointRegister) error {
	return nil
}

/*
ReadKey - Simply return the value of a particular key, or an error.
*/
func (s *Redis) ReadKey(key string) (string, error) {
	conn := s.pool.Get()
	defer conn.Close()

	reply, err := redis.String(conn.Do("GET", key))
	if err != nil {
		return "", err
	}
	return reply, nil
}

/*
DeleteKey - Deletes an existing key.
*/
func (s *Redis) DeleteKey(key string) error {
	conn := s.pool.Get()
	defer conn.Close()

	reply, err := redis.Int(conn.Do("DEL", key))
	if err != nil {
		return err
	}
	if 0 == reply {
		return ErrNoKey
	}
	return nil
}

/*--------------------------------------------------------------------------------------------------
 */
