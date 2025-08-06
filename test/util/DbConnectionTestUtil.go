/*
Copyright 2021 Reactive Tech Limited.
"Reactive Tech Limited" is a company located in England, United Kingdom.
https://www.reactive-tech.io

Lead Developer: Alex Arica

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package util

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strconv"

	_ "github.com/lib/pq"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"reactive-tech.io/kubegres/test/resourceConfigs"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type DbConnectionDbUtil struct {
	Port               int
	LogLabel           string
	IsPrimaryDb        bool
	NbreInsertedUsers  int
	LastInsertedUserId string
	db                 *sql.DB
	kubegresName       string
	serviceToQueryDb   runtime.Object
	resourceCreator    TestResourceCreator
	k8sClient          client.Client
}

type AccountUser struct {
	UserId, Username string
}

func InitDbConnectionDbUtil(resourceCreator TestResourceCreator, kubegresName string, nodePort int, isPrimaryDb bool, k8sClient client.Client) DbConnectionDbUtil {

	serviceToQueryDb, err := resourceCreator.CreateServiceToSqlQueryDb(kubegresName, nodePort, isPrimaryDb)
	if err != nil {
		log.Fatal("Unable to create a Service on port '"+strconv.Itoa(nodePort)+"' to query DB.", err)
	}

	logLabel := "Primary " + kubegresName
	if !isPrimaryDb {
		logLabel = "Replica " + kubegresName
	}

	db := DbConnectionDbUtil{
		Port:             nodePort,
		LogLabel:         logLabel,
		IsPrimaryDb:      isPrimaryDb,
		kubegresName:     kubegresName,
		serviceToQueryDb: serviceToQueryDb,
		resourceCreator:  resourceCreator,
		k8sClient:        k8sClient,
	}

	db.exportNodeAddressAndPort()

	return db
}

func InitExternalDbConnectionDbUtil(resourceCreator TestResourceCreator, nodePort int, k8sClient client.Client) DbConnectionDbUtil {
	serviceToQueryDb, err := resourceCreator.CreateServiceToSqlQueryExternalDb(nodePort)
	if err != nil {
		log.Fatal("Unable to create a Service on port '"+strconv.Itoa(nodePort)+"' to query external DB.", err)
	}

	return DbConnectionDbUtil{
		Port:             nodePort,
		LogLabel:         "External DB",
		IsPrimaryDb:      true,
		kubegresName:     "External DB",
		serviceToQueryDb: serviceToQueryDb,
		resourceCreator:  resourceCreator,
		k8sClient:        k8sClient,
	}
}

func (r *DbConnectionDbUtil) Close() {
	r.logInfo("Closing DB connection '" + r.LogLabel + "'")
	err := r.db.Close()
	if err != nil {
		r.logError("Error while closing DB connection: ", err)
	} else {
		r.logInfo("Closed")
	}
}

func (r *DbConnectionDbUtil) InsertUser() bool {
	if !r.connect() {
		return false
	}

	newUserId, _ := strconv.Atoi(r.LastInsertedUserId)
	newUserId++
	newUserIdStr := strconv.Itoa(newUserId)
	accountUser := AccountUser{
		UserId:   newUserIdStr,
		Username: "username" + newUserIdStr,
	}

	sqlQuery := "INSERT INTO " + resourceConfigs.TableName + " VALUES (" + accountUser.UserId + ", '" + accountUser.Username + "');"
	_, err := r.db.Exec(sqlQuery)
	if err != nil {
		r.logError("Error of query: "+sqlQuery+" ", err)
		return false
	}

	r.logInfo("Success of: " + sqlQuery)
	r.NbreInsertedUsers++
	r.LastInsertedUserId = newUserIdStr
	return true
}

func (r *DbConnectionDbUtil) exportNodeAddressAndPort() (string, int, bool) {
	var nodeList v1.NodeList
	err := r.k8sClient.List(context.Background(), &nodeList)
	if err != nil {
		r.logError("Unable to list nodes in the cluster", err)
		return "", 0, false
	}
	if len(nodeList.Items) == 0 {
		r.logError("No nodes found in the cluster", nil)
		return "", 0, false
	}
	node := nodeList.Items[0]
	var nodeAddress string
	for _, address := range node.Status.Addresses {
		if address.Type != v1.NodeInternalIP {
			continue
		}
		nodeAddress = address.Address
		break
	}

	if nodeAddress == "" {
		r.logError("No internal IP address found for the node", nil)
		return "", 0, false
	}

	if r.IsPrimaryDb {
		_ = os.Setenv("TEST_PRIMARY_HOSTNAME", nodeAddress)
		_ = os.Setenv("TEST_PRIMARY_PORT", strconv.Itoa(r.Port))
	}

	return nodeAddress, r.Port, true

}

func (r *DbConnectionDbUtil) connect() bool {

	nodeAddress, port, ok := r.exportNodeAddressAndPort()
	if !ok {
		r.logError("Failed to export node address and port", nil)
		return false
	}

	psqlInfo := fmt.Sprintf("host=%s port=%d user=%s "+
		"password=%s dbname=%s sslmode=disable",
		nodeAddress, port, resourceConfigs.DbUser, resourceConfigs.DbPassword, resourceConfigs.DbName)

	if r.ping(psqlInfo) {
		return true
	}

	var err error
	r.db, err = sql.Open("postgres", psqlInfo)
	if err != nil {
		r.logError("Unable to connect to: "+psqlInfo, err)
		_ = r.db.Close()
		return false
	}

	if !r.ping(psqlInfo) {
		return false
	}

	if r.IsPrimaryDb && !r.createTableIfDoesItNotExist() {
		return false
	}

	r.logInfo("Successfully connected to PostgreSql with port: '" + strconv.Itoa(port) + "'")

	users := r.GetUsers()
	r.NbreInsertedUsers = len(users)
	if r.NbreInsertedUsers > 0 {
		lastUserIndex := r.NbreInsertedUsers - 1
		r.LastInsertedUserId = users[lastUserIndex].UserId
	} else {
		r.LastInsertedUserId = "0"
	}

	return true
}

func (r *DbConnectionDbUtil) ping(psqlInfo string) bool {
	if r.db == nil {
		return false
	}

	err := r.db.Ping()
	if err != nil {
		r.logError("Ping failed to: "+psqlInfo, err)
		_ = r.db.Close()
		return false
	}
	r.logError("Ping succeeded to: "+psqlInfo, err)
	return true
}

func (r *DbConnectionDbUtil) DeleteUser(userIdToDelete string) bool {
	if !r.connect() {
		return false
	}

	sqlQuery := "DELETE FROM " + resourceConfigs.TableName + " WHERE user_id=" + userIdToDelete
	_, err := r.db.Exec(sqlQuery)
	if err != nil {
		r.logError("Error of query: "+sqlQuery+" ", err)
		return false
	}

	r.logInfo("Success of: " + sqlQuery)
	r.NbreInsertedUsers--
	return true
}

func (r *DbConnectionDbUtil) GetUsers() []AccountUser {

	var accountUsers []AccountUser

	if !r.connect() {
		return accountUsers
	}

	sqlQuery := "SELECT user_id, username FROM " + resourceConfigs.TableName
	rows, err := r.db.Query(sqlQuery)
	if err != nil {
		r.logError("Error of query: "+sqlQuery+" ", err)
		return accountUsers
	}

	r.logInfo("Success of: " + sqlQuery)

	defer rows.Close()

	for rows.Next() {

		accountUser := AccountUser{}
		err := rows.Scan(&accountUser.UserId, &accountUser.Username)

		if err != nil {
			r.logError("Error while retrieving a user's row: ", err)
			return accountUsers
		}

		accountUsers = append(accountUsers, accountUser)
		r.logInfo("account.userId: '" + accountUser.UserId + "', account.username: '" + accountUser.Username + "'")
	}

	err = rows.Err()
	if err != nil {
		r.logError("Row error: ", err)
	}

	return accountUsers
}

func (r *DbConnectionDbUtil) createTableIfDoesItNotExist() bool {

	sqlQuery := "SELECT to_regclass('" + resourceConfigs.TableName + "');"
	rows, err := r.db.Query(sqlQuery)
	if err != nil {
		r.logError("Error of query: "+sqlQuery+" ", err)
		return false
	}

	var tableFound sql.NullString
	for rows.Next() {
		err = rows.Scan(&tableFound)
		if err != nil {
			r.logError("Error of query: "+sqlQuery+" ", err)
			return false
		}
	}

	if !tableFound.Valid {
		sqlQuery = "CREATE TABLE " + resourceConfigs.TableName + "(user_id serial PRIMARY KEY, username VARCHAR (50) NOT NULL);"
		_, err = r.db.Exec(sqlQuery)
		if err != nil {
			r.logError("Error of query: "+sqlQuery+" ", err)
			return false
		}
		r.logInfo("Table " + resourceConfigs.TableName + " created")
	}

	return true
}

func (r *DbConnectionDbUtil) logInfo(msg string) {
	log.Println(r.LogLabel + " - " + msg)
}

func (r *DbConnectionDbUtil) logError(msg string, err error) {
	log.Println(r.LogLabel+" - "+msg+" ", err)
}
