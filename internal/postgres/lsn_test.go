package postgres_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"reactive-tech.io/kubegres/internal/postgres"
)

func TestParseLSN(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    postgres.LSN
		wantErr bool
	}{
		{name: "empty means never replayed", in: "", want: 0},
		{name: "whitespace only", in: "   ", want: 0},
		{name: "zero", in: "0/0", want: 0},
		{name: "low half only", in: "0/3C", want: 0x3C},
		{name: "both halves", in: "16/B374D848", want: 0x16B374D848},
		{name: "lower case hex", in: "16/b374d848", want: 0x16B374D848},
		{name: "max", in: "FFFFFFFF/FFFFFFFF", want: postgres.LSN(0xFFFFFFFFFFFFFFFF)},
		{name: "no separator", in: "3C", wantErr: true},
		{name: "high half not hex", in: "zz/0", wantErr: true},
		{name: "low half not hex", in: "0/zz", wantErr: true},
		{name: "high half overflows 32 bits", in: "1FFFFFFFF/0", wantErr: true},
		{name: "low half overflows 32 bits", in: "0/1FFFFFFFF", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := postgres.ParseLSN(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestLSNOrderingIsNumericNotLexicographic(t *testing.T) {
	// "0/9" sorts after "0/10" as a string but before it as an LSN. Selection compares parsed
	// values, so this cannot pick the wrong replica.
	smaller, err := postgres.ParseLSN("0/9")
	require.NoError(t, err)
	larger, err := postgres.ParseLSN("0/10")
	require.NoError(t, err)

	require.Less(t, smaller, larger)
}

func TestLSNString(t *testing.T) {
	lsn, err := postgres.ParseLSN("16/B374D848")
	require.NoError(t, err)
	require.Equal(t, "16/B374D848", lsn.String())
	require.Equal(t, "0/0", postgres.LSN(0).String())
}

func TestLSNDistanceIsSymmetricAndNonNegative(t *testing.T) {
	behind, err := postgres.ParseLSN("0/3A")
	require.NoError(t, err)
	ahead, err := postgres.ParseLSN("0/3C")
	require.NoError(t, err)

	require.Equal(t, int64(2), behind.Distance(ahead))
	require.Equal(t, int64(2), ahead.Distance(behind))
	require.Equal(t, int64(0), ahead.Distance(ahead))
}

func TestMax(t *testing.T) {
	require.Equal(t, postgres.LSN(5), postgres.Max(5, 3))
	require.Equal(t, postgres.LSN(5), postgres.Max(3, 5))
}

func TestIsZero(t *testing.T) {
	require.True(t, postgres.LSN(0).IsZero())
	require.False(t, postgres.LSN(1).IsZero())
}
