package evolution_loop_test

import (
	"testing"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop"
)

// compile-time assertion: *DBEvolutionStore satisfies EvolutionStore.
var _ evolution_loop.EvolutionStore = (*evolution_loop.DBEvolutionStore)(nil)

func TestNewDBEvolutionStore_ReturnsNonNil(t *testing.T) {
	database := &db.DB{}
	store := evolution_loop.NewDBEvolutionStore(database)
	if store == nil {
		t.Fatal("NewDBEvolutionStore returned nil")
	}
}

func TestDBEvolutionStore_ImplementsInterface(t *testing.T) {
	var _ evolution_loop.EvolutionStore = evolution_loop.NewDBEvolutionStore(&db.DB{})
}
