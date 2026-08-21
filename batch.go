package main

import (
	"context"
	"errors"
	"fmt"
)

func prepareConversion(files []string, options ConversionOptions, settings Settings) ([]string, ConversionOptions, error) {
	files = uniquePaths(files)
	if len(files) == 0 {
		return nil, ConversionOptions{}, errors.New("choose at least one image")
	}

	format := NormalizeFormat(options.Format)
	if format == "" {
		return nil, ConversionOptions{}, fmt.Errorf("unsupported output format %q", options.Format)
	}
	options.Format = string(format)
	if options.Quality < 1 {
		switch format {
		case FormatJPEG:
			options.Quality = settings.JPEGQuality
		case FormatWebP:
			options.Quality = settings.WebPQuality
		}
	}
	return files, options, nil
}

func convertBatch(
	ctx context.Context,
	files []string,
	options ConversionOptions,
	onResult func(FileResult),
) ConversionSummary {
	summary := ConversionSummary{Total: len(files), Results: make([]FileResult, 0, len(files))}

	for _, input := range files {
		var result FileResult
		if ctx.Err() != nil {
			result = resultForError(input, context.Canceled)
			summary.Canceled++
		} else {
			converted, err := ConvertFile(ctx, input, options)
			result = converted
			if err != nil {
				result = resultForError(input, err)
				if errors.Is(err, context.Canceled) {
					summary.Canceled++
				} else {
					summary.Failed++
				}
			} else {
				summary.Completed++
			}
		}
		summary.Results = append(summary.Results, result)
		if onResult != nil {
			onResult(result)
		}
	}
	return summary
}
