package data

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"zuri.chat/zccore/plugin"
	"zuri.chat/zccore/utils"
)

type writeDataRequest struct {
	PluginID       string                 `json:"plugin_id"`
	CollectionName string                 `json:"collection_name"`
	OrganizationID string                 `json:"organization_id"`
	BulkWrite      bool                   `json:"bulk_write"`
	ObjectID       string                 `json:"object_id,omitempty"`
	Filter         map[string]interface{} `json:"filter"`
	Payload        interface{}            `json:"payload,omitempty"`
	RawQuery       interface{}            `json:"raw_query,omitempty"`
}

// WriteData handles data mutation operations(write, update, delete) for plugins.
func WriteData(w http.ResponseWriter, r *http.Request) {
	reqData := new(writeDataRequest)

	if err := utils.ParseJSONFromRequest(r, reqData); err != nil {
		utils.GetError(fmt.Errorf("error processing request: %v", err), http.StatusUnprocessableEntity, w)
		return
	}

	if _, err := plugin.FindPluginByID(r.Context(), reqData.PluginID); err != nil {
		msg := "plugin with this id does not exist"
		utils.GetError(errors.New(msg), http.StatusNotFound, w)

		return
	}

	// if plugin is writing to this collection the first time, we create a record linking this collection to the plugin.
	if !pluginHasCollection(reqData.PluginID, reqData.OrganizationID, reqData.CollectionName) {
		if err := createPluginCollectionRecord(reqData.PluginID, reqData.OrganizationID, reqData.CollectionName); err != nil {
			utils.GetError(err, http.StatusInternalServerError, w)
			return
		}
	}

	w.Header().Set("content-type", "application/json")

	switch r.Method {
	case "POST":
		reqData.handlePost(w, r)
	case "PUT", "PATCH":
		reqData.handlePut(w, r)
	default:
		fmt.Fprint(w, `{"data_write": "Data write request"}`)
	}
}

func (wdr *writeDataRequest) handlePost(w http.ResponseWriter, _ *http.Request) {
	var payload interface{}

	if wdr.BulkWrite {
		payload = wdr.Payload
	} else {
		payload = []interface{}{wdr.Payload}
	}

	res, err := insertMany(wdr.prefixCollectionName(), payload)
	if err != nil {
		utils.GetError(fmt.Errorf("an error occurred: %v", err), http.StatusInternalServerError, w)
		return
	}

	data := utils.M{
		"insert_count": len(res.InsertedIDs),
	}

	if wdr.BulkWrite {
		data["object_ids"] = res.InsertedIDs
	} else {
		data["object_id"] = res.InsertedIDs[0]
	}

	w.WriteHeader(http.StatusCreated)
	utils.GetSuccess("success", data, w)
}

func (wdr *writeDataRequest) handlePut(w http.ResponseWriter, _ *http.Request) {
	var err error

	var res *mongo.UpdateResult

	filter := make(map[string]interface{})
	collName := wdr.prefixCollectionName()

	if wdr.ObjectID != "" {
		filter["_id"] = wdr.ObjectID
	} else {
		filter = wdr.Filter
	}

	filter["deleted"] = bson.M{"$ne": true}
	normalizeIDIfExists(filter)

	if wdr.RawQuery != nil {
		res, err = rawQueryupdateMany(collName, filter, wdr.RawQuery)
	} else {
		res, err = updateMany(collName, filter, wdr.Payload)
	}

	if err != nil {
		utils.GetError(fmt.Errorf("an error occurred: %v", err), http.StatusInternalServerError, w)
		return
	}

	data := utils.M{
		"matched_documents":  res.MatchedCount,
		"modified_documents": res.ModifiedCount,
	}

	utils.GetSuccess("success", data, w)
}

func (wdr *writeDataRequest) prefixCollectionName() string {
	return getPrefixedCollectionName(wdr.PluginID, wdr.OrganizationID, wdr.CollectionName)
}

func insertMany(collName string, data interface{}) (*mongo.InsertManyResult, error) {
	docs, ok := data.([]interface{})

	if !ok {
		return nil, errors.New("invalid object type, payload must be an array of objects")
	}

	return utils.CreateManyMongoDBDocs(collName, docs)
}

func updateMany(collName string, filter map[string]interface{}, upd interface{}) (*mongo.UpdateResult, error) {
	update, ok := upd.(map[string]interface{})

	if !ok {
		return nil, errors.New("invalid object type")
	}

	return utils.UpdateManyMongoDBDocs(collName, filter, update)
}

func mustObjectIDFromHex(hex string) primitive.ObjectID {
	objID, err := primitive.ObjectIDFromHex(hex)

	if err != nil {
		panic(err)
	}

	return objID
}

func rawQueryupdateMany(collName string, filter map[string]interface{}, rawQuery interface{}) (*mongo.UpdateResult, error) {
	coll := utils.GetCollection(collName)
	return coll.UpdateMany(context.TODO(), filter, rawQuery)
}

func normalizeIDIfExists(filter map[string]interface{}) {
	if id, exists := filter["_id"]; exists {
		filter["_id"] = mustObjectIDFromHex(id.(string))
		return
	}

	if id, exists := filter["id"]; exists {
		delete(filter, "id")
		filter["_id"] = mustObjectIDFromHex(id.(string))

		return
	}
}
