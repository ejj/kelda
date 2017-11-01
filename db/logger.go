package db

import (
	"sort"
	"strings"

	log "github.com/sirupsen/logrus"
)

func (conn Conn) runLogger() {
	for _, t := range AllTables {
		t := t
		go func() {
			for range conn.Trigger(t).C {
				conn.logTable(t)
			}
		}()
	}
}

func (conn Conn) logTable(t TableType) {
	var truncated bool
	var strs []string
	conn.Txn(t).Run(func(view Database) error {
		var rows []row
		for _, v := range view.tables[t].rows {
			if len(rows) > 50 {
				truncated = true
				break
			}

			rows = append(rows, v)
		}

		sort.Sort(rowSlice(rows))
		for _, r := range rows {
			strs = append(strs, r.String())
		}
		return nil
	})

	if truncated {
		strs = append(strs, "Truncated ...")
	}

	log.Infof("%s:\n\t%s", t, strings.Join(strs, "\n\t"))

}
