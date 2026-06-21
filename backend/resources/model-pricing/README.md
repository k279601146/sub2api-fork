# Model Pricing Data

This directory contains a local fallback copy of the configured model pricing mirror.

## Source
The original pricing data is maintained by the LiteLLM project, but this project uses a curated mirror configured by `pricing.remote_url` in `backend/config.yaml`:
- Configured mirror: https://raw.githubusercontent.com/Wei-Shaw/model-price-repo/refs/heads/main//model_prices_and_context_window.json
- Mirror hash: https://raw.githubusercontent.com/Wei-Shaw/model-price-repo/refs/heads/main//model_prices_and_context_window.sha256
- LiteLLM upstream source: https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json

The LiteLLM upstream file is much larger because it includes many extra model entries and fields. This fallback file should match the configured mirror unless `pricing.remote_url` is intentionally changed.

## Purpose
This local copy serves as a fallback when the configured remote file cannot be downloaded due to:
- Network restrictions
- Firewall rules
- DNS resolution issues
- GitHub being blocked in certain regions
- Docker container network limitations

## Update Process
The pricingService will:
1. Load the runtime cache from `pricing.data_dir` (`./data/model_pricing.json` by default) when available
2. Compare the loaded data with the configured remote hash
3. Download the configured remote file when the hash differs
4. If the initial download/load fails, use this local fallback file
5. Log a warning when using the fallback file

## Manual Update
To manually update this fallback file with the same data used by the runtime configuration:
```bash
curl -s https://raw.githubusercontent.com/Wei-Shaw/model-price-repo/refs/heads/main//model_prices_and_context_window.json -o model_prices_and_context_window.json
```

If you also need to refresh the runtime cache used by a local backend process, update `./data/model_pricing.json` with the same file and write the matching SHA256 to `./data/model_pricing.sha256`.

## File Format
The file contains JSON data with model pricing information including:
- Model names and identifiers
- Input/output token costs
- Context window sizes
- Model capabilities

Last updated: 2026-06-21
