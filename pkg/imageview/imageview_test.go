package imageview

import (
	"image"
	"image/color"
	"testing"

	"github.com/diamondburned/gotk4/pkg/gsk/v4"
	"libdb.so/gotk4-imageview/pkg/imgload"
)

func TestSelectScalingFilter(t *testing.T) {
	tests := []struct {
		name              string
		texW, texH        int
		drawnW, drawnH    float64
		scaleFactor       int
		wantScalingFilter gsk.ScalingFilter
	}{
		{
			name:              "ZoomedIn-2x-At-1xScale-UsesNearest",
			texW:              200,
			texH:              200,
			drawnW:            400,
			drawnH:            400,
			scaleFactor:       1,
			wantScalingFilter: gsk.ScalingFilterNearest,
		},
		{
			name:              "1to1-Logical-At-2xHiDPI-UsesNearest",
			texW:              200,
			texH:              200,
			drawnW:            200,
			drawnH:            200,
			scaleFactor:       2,
			wantScalingFilter: gsk.ScalingFilterNearest,
		},
		{
			name:              "1to1-Physical-At-2xHiDPI-UsesLinear",
			texW:              400,
			texH:              400,
			drawnW:            200,
			drawnH:            200,
			scaleFactor:       2,
			wantScalingFilter: gsk.ScalingFilterLinear,
		},
		{
			name:              "MildDownscale-0.75x-Physical-UsesLinear",
			texW:              1000,
			texH:              1000,
			drawnW:            750,
			drawnH:            750,
			scaleFactor:       1,
			wantScalingFilter: gsk.ScalingFilterLinear,
		},
		{
			name:              "HeavyDownscale-0.25x-Physical-UsesTrilinear",
			texW:              4000,
			texH:              3000,
			drawnW:            800,
			drawnH:            600,
			scaleFactor:       1,
			wantScalingFilter: gsk.ScalingFilterTrilinear,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SelectScalingFilter(tc.texW, tc.texH, tc.drawnW, tc.drawnH, tc.scaleFactor)
			if got != tc.wantScalingFilter {
				t.Errorf("SelectScalingFilter(%d, %d, %v, %v, %d) = %v, want %v",
					tc.texW, tc.texH, tc.drawnW, tc.drawnH, tc.scaleFactor, got, tc.wantScalingFilter)
			}
		})
	}
}

func TestPaintableRotationAndDimensions(t *testing.T) {
	rgba := image.NewRGBA(image.Rect(0, 0, 120, 80))
	rgba.SetRGBA(0, 0, color.RGBA{R: 255, A: 255})
	pb := imgload.ConvertImage(rgba, imgload.OrientationNormal, nil)
	img := &imgload.Image{
		Format:      "png",
		Orientation: imgload.OrientationNormal,
		Static:      pb,
	}

	p := NewPaintableFromImage(img)
	defer p.Clear()

	steps := []struct {
		rot   int
		wantW int
		wantH int
	}{
		{0, 120, 80},
		{90, 80, 120},
		{180, 120, 80},
		{270, 80, 120},
		{360, 120, 80},
		{-90, 80, 120},
	}

	for _, tc := range steps {
		p.SetRotation(tc.rot)
		if gotW, gotH := p.IntrinsicWidth(), p.IntrinsicHeight(); gotW != tc.wantW || gotH != tc.wantH {
			t.Errorf("SetRotation(%d): IntrinsicSize = %dx%d, want %dx%d",
				tc.rot, gotW, gotH, tc.wantW, tc.wantH)
		}
	}
}
