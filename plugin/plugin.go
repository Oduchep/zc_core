package plugin

import (
	"log"
	"net/http"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	"go.mongodb.org/mongo-driver/bson"
	"zuri.chat/zccore/utils"
)

type M = map[string]interface{}

var validate = validator.New()

func Register(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("content-type", "application/json")

	p := Plugin{}

	if err := utils.ParseJSONFromRequest(r, &p); err != nil {
		utils.GetError(err, http.StatusUnprocessableEntity, w)
		return
	}

	if err := validate.Struct(p); err != nil {
		utils.GetError(err, http.StatusBadRequest, w)
		return
	}

	if ps, err := FindPlugins(r.Context(), bson.M{"template_url": p.TemplateURL}); err == nil && len(ps) > 0 {
		utils.GetError(errors.New("duplicate plugin registration"), http.StatusForbidden, w)
		return
	}

	if err := CreatePlugin(r.Context(), &p); err != nil {
		utils.GetError(err, http.StatusInternalServerError, w)
		return
	}

	w.WriteHeader(201)

	utils.GetSuccess("success", M{"plugin_id": p.ID.Hex()}, w)

	go approvePlugin(p.ID.Hex())
}

// a hack to simulate plugin approval, it basically waits 10 seconds after creation and approves the plugin.
func approvePlugin(id string) {
	const ten = 10

	time.Sleep(ten * time.Second)

	update := M{"approved": true, "deleted": false, "approved_at": time.Now().String()}

	_, err := utils.UpdateOneMongoDBDoc(PluginCollectionName, id, update)

	if err != nil {
		log.Println("error approving plugin")
		return
	}

	log.Printf("Plugin %s approved\n", id)
}

func Update(w http.ResponseWriter, r *http.Request) {
	pp := Patch{}
	id := mux.Vars(r)["id"]

	if err := utils.ParseJSONFromRequest(r, &pp); err != nil {
		utils.GetError(errors.WithMessage(err, "error processing request"), http.StatusUnprocessableEntity, w)
		return
	}

	if err := updatePlugin(r.Context(), id, &pp); err != nil {
		utils.GetError(errors.WithMessage(err, "cannot update, bad request"), http.StatusBadRequest, w)
		return
	}

	utils.GetSuccess("updated plugin successfully", nil, w)
}

func Delete(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	_, err := utils.UpdateOneMongoDBDoc("plugins", id, M{"deleted": true, "deleted_at": time.Now().String()})

	if err != nil {
		utils.GetError(errors.WithMessage(err, "error deleting plugin"), http.StatusBadRequest, w)
		return
	}

	w.WriteHeader(http.StatusNoContent)

	w.Header().Set("content-type", "application/json")

	utils.GetSuccess("plugin deleted", nil, w)
}
