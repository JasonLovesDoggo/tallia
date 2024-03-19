package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/google/uuid"

	"github.com/jasonlovesdoggo/abacus/utils"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

var Client *redis.Client

func init() {
	// Connect to Redis
	utils.LoadEnv()
	ADDR := os.Getenv("REDIS_HOST") + ":" + os.Getenv("REDIS_PORT")
	fmt.Println("Listening to redis on: " + ADDR)
	PASS, _ := strconv.Atoi(os.Getenv("REDIS_DB"))
	Client = redis.NewClient(&redis.Options{
		Addr:     ADDR, // Redis server address
		Username: os.Getenv("REDIS_USERNAME"),
		Password: os.Getenv("REDIS_PASSWORD"),
		DB:       PASS,
	})
}

func HitView(c *gin.Context) {
	namespace, key := utils.GetNamespaceKey(c)
	if namespace == "" || key == "" {
		return
	}
	//fmt.Println("namespace:"+namespace, "key:"+key)
	dbKey := utils.CreateKey(c, namespace, key, false)
	if dbKey == "" { // error is handled in CreateKey
		return
	}
	// Get data from Redis
	val, err := Client.Incr(context.Background(), dbKey).Result()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get data. Try again later."})
		return
	}
	go func() {
		Client.Expire(context.Background(), dbKey, utils.BaseTTLPeriod)
	}()

	c.JSON(http.StatusOK, gin.H{"value": val})
}

func CreateRandomView(c *gin.Context) {
	key, _ := utils.GenerateRandomString(16)
	namespace, err := utils.GenerateRandomString(16)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate random string. Try again later."})
		return
	}

	c.Params = gin.Params{gin.Param{Key: "namespace", Value: namespace}, gin.Param{Key: "key", Value: key}}
	CreateView(c)
}
func CreateView(c *gin.Context) {
	namespace, key := utils.GetNamespaceKey(c)
	if namespace == "" || key == "" {
		return
	}
	dbKey := utils.CreateKey(c, namespace, key, false)
	if dbKey == "" { // error is handled in CreateKey
		return
	}
	initialValue, err := strconv.Atoi(c.DefaultQuery("initializer", "0"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "initializer must be a number"})
		return
	}
	// Get data from Redis
	created := Client.SetNX(context.Background(), dbKey, initialValue, utils.BaseTTLPeriod)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to set data. Try again later."})
		return
	}
	if created.Val() == false {
		c.JSON(http.StatusConflict, gin.H{"error": "Key already exists, please use a different key."})
		return
	}
	AdminKey := uuid.New().String()                                            // Create a new admin key used for deletion and control
	Client.Set(context.Background(), utils.CreateAdminKey(dbKey), AdminKey, 0) // todo: figure out how to handle admin keys (handle alongside admin orrrrrrr separately as in a routine once a month that deletes all admin keys with no corresponding key)
	c.JSON(http.StatusCreated, gin.H{"key": key, "namespace": namespace, "admin_key": AdminKey, "value": initialValue})
}

func InfoView(c *gin.Context) { // todo: write docs on what negative values mean (https://redis.io/commands/ttl/)
	namespace, key := utils.GetNamespaceKey(c)
	if namespace == "" || key == "" {
		return
	}
	dbKey := utils.CreateKey(c, namespace, key, true)
	if dbKey == "" { // error is handled in CreateKey
		return
	}
	dbValue := Client.Get(context.Background(), dbKey).Val()
	count, _ := strconv.Atoi(dbValue)

	isGenuine := Client.Exists(context.Background(), utils.CreateAdminKey(dbKey)).Val() == 0
	fmt.Println(Client.Exists(context.Background(), utils.CreateAdminKey(dbKey)).Val())
	timeToLive := Client.TTL(context.Background(), dbKey).Val()
	exists := timeToLive != -2
	if !exists {
		count = -1
	}
	c.JSON(http.StatusOK, gin.H{"value": count, "full_key": dbKey, "is_genuine": isGenuine, "expires": timeToLive, "exists": exists})
}

func DeleteView(c *gin.Context) {
	namespace, key := utils.GetNamespaceKey(c)
	if namespace == "" || key == "" {
		return
	}
	authToken := c.DefaultQuery("token", "")
	if authToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "token is required, please provide a token in the fmt of ?token=ADMIN_TOKEN"})
		return
	}
	createKey := utils.CreateKey(c, namespace, key, true)
	if createKey == "" { // error is handled in CreateKey
		return
	}
	adminDBKey := utils.CreateAdminKey(createKey)
	adminKey, err := Client.Get(context.Background(), adminDBKey).Result()
	if errors.Is(err, redis.Nil) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "This entry is genuine and does not have an admin key. You cannot delete it. If you wanted to delete it, you should have created it with the /create endpoint."})

	} else if adminKey != authToken {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "token is invalid"})
	} else {
		Client.Del(context.Background(), createKey)  // Delete the normal key
		Client.Del(context.Background(), adminDBKey) // delete the admin key as it's now useless
		c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "Deleted key: " + createKey})
	}
}
