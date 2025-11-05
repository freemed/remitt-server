package model

const (
	TABLE_PLUGIN_OPTIONS = "tPluginOptions"
)

type PluginOptionsModel struct {
	PluginOption string     `db:"poption"`
	Plugin       string     `db:"plugin"`
	FullName     string     `db:"fullname"`
	Version      string     `db:"version"`
	Author       string     `db:"author"`
	Category     string     `db:"category"`
	InputFormat  NullString `db:"inputFormat"`
	OutputFormat NullString `db:"outputFormat"`
}

func init() {
	DbTables = append(DbTables, DbTable{TableName: TABLE_PLUGIN_OPTIONS, Obj: PluginOptionsModel{}, Key: ""})
}

func GetPluginOptions(plugin string) ([]PluginOptionsModel, error) {
	var o []PluginOptionsModel
	_, err := DbMap.Select(&o, "SELECT * FROM "+TABLE_PLUGIN_OPTIONS+" WHERE plugin = ?", plugin)
	return o, err
}
