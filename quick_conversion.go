package main

import (
	"context"
	"errors"
	"fmt"
)

func runQuickConversion(ctx context.Context, request LaunchRequest, settings Settings) (ConversionSummary, error) {
	options := ConversionOptions{
		Format:           request.Format,
		PreserveMetadata: settings.PreserveMetadata,
	}
	files, options, err := prepareConversion(request.Files, options, settings)
	if err != nil {
		return ConversionSummary{}, err
	}

	summary := convertBatch(ctx, files, options, nil)
	return summary, conversionSummaryError(summary)
}

func executeQuickConversion(request LaunchRequest) error {
	settings, err := loadSettings()
	if err != nil {
		fmt.Println("ConvertMe settings could not be loaded; using defaults:", err)
		settings = defaultSettings()
	}

	conversionErr, notifyErr := runQuickConversionAndNotify(
		context.Background(),
		request,
		settings,
		showQuickConversionNotification,
	)
	if notifyErr != nil {
		fmt.Println("ConvertMe notification could not be shown:", notifyErr)
	}
	return conversionErr
}

func runQuickConversionAndNotify(
	ctx context.Context,
	request LaunchRequest,
	settings Settings,
	notify func(ConversionSummary, ImageFormat, bool) error,
) (conversionErr, notificationErr error) {
	summary, conversionErr := runQuickConversion(ctx, request, settings)
	if summary.Total == 0 {
		return conversionErr, nil
	}
	notificationErr = notify(summary, NormalizeFormat(request.Format), settings.ShowNotifications)
	return conversionErr, notificationErr
}

func conversionSummaryError(summary ConversionSummary) error {
	if summary.Failed == 0 && summary.Canceled == 0 {
		return nil
	}

	for _, result := range summary.Results {
		if result.Error != "" {
			return fmt.Errorf("%d of %d image(s) could not be converted: %w", summary.Failed+summary.Canceled, summary.Total, errors.New(result.Error))
		}
	}
	return fmt.Errorf("%d of %d image(s) could not be converted", summary.Failed+summary.Canceled, summary.Total)
}
