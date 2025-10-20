package astiav

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func testPacketSideData(sd *PacketSideData, t *testing.T) {
	m1 := NewDisplayMatrixFromRotation(90)
	require.NoError(t, sd.DisplayMatrix().Add(m1))
	m2, ok := sd.DisplayMatrix().Get()
	require.True(t, ok)
	require.Equal(t, m1.Rotation(), m2.Rotation())
}

func TestPacketSideData(t *testing.T) {
	cp := AllocCodecParameters()
	defer cp.Free()
	sd := cp.SideData()

	m1, ok := sd.DisplayMatrix().Get()
	require.False(t, ok)
	require.Nil(t, m1)
	m1 = NewDisplayMatrixFromRotation(90)
	require.NoError(t, sd.DisplayMatrix().Add(m1))
	m2, ok := sd.DisplayMatrix().Get()
	require.True(t, ok)
	require.Equal(t, m1.Rotation(), m2.Rotation())

	ss1, ok := sd.SkipSamples().Get()
	require.False(t, ok)
	require.Nil(t, ss1)
	ss1 = &SkipSamples{
		ReasonEnd:   1,
		ReasonStart: 2,
		SkipEnd:     3,
		SkipStart:   4,
	}
	require.NoError(t, sd.SkipSamples().Add(ss1))
	ss2, ok := sd.SkipSamples().Get()
	require.True(t, ok)
	require.Equal(t, ss1, ss2)
}
