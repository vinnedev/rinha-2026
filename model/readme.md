# Fraud Detection — Decision Tree

Classifier (DT/RF) treinado nos 3M vetores de `resources/references.json.gz` da Rinha de Backend 2026.

## Estrutura

| Arquivo | Para quê |
|---|---|
| `fraud_dt.py` | Biblioteca importável — `load_dataset`, `dataset_stats`, `analyze_sentinels`, `fit_tree`, `fit_forest`, `sweep_hyperparams`, `evaluate`, `save_model`, `load_model`, `predict_samples`, `benchmark_inference` |
| `train.py` | CLI: treina + salva `fraud_dt.joblib` + `metrics.json` |
| `predict.py` | CLI: carrega o modelo e classifica amostras |
| `fraud-detection.ipynb` | Notebook: EDA → tratamento → sweep → DT + RF → avaliação → uso |
| `pyproject.toml` | Deps gerenciadas por `uv` |
| `fraud_dt.joblib` | Modelo treinado |
| `metrics.json` | Métricas da última run de treino |

## Setup (`uv`)

```bash
cd model
uv sync                              # instala deps + ipykernel (sempre)
uv sync --group lab                  # opcional: jupyterlab no próprio venv
uv run python -m ipykernel install --user --name rinha-fraud-dt --display-name "Python (rinha-fraud-dt)"
```

A última linha registra o venv como **kernel** disponível em qualquer Jupyter (system/Homebrew, VS Code, `uv run jupyter lab`, …). O notebook já vem amarrado nesse kernel — abrindo ele, o Jupyter usa o Python do venv automaticamente.

> `ipykernel` está nas dependências base — qualquer `uv sync` mantém o kernel funcionando. Se um dia o erro `No module named ipykernel_launcher` voltar, rode `uv sync` novamente.

## Uso

### CLI
```bash
uv run python train.py                # treino completo + persistência
uv run python train.py --max-depth 24 --min-samples-leaf 20
uv run python predict.py              # classifica os exemplos da spec
```

### Notebook
```bash
uv run jupyter lab fraud-detection.ipynb
```

Ou abra com seu Jupyter habitual e selecione o kernel **Python (rinha-fraud-dt)**.

### Importando em outros scripts/notebooks
```python
from fraud_dt import load_dataset, fit_tree, evaluate, save_model, load_model, predict_samples

X, y = load_dataset()                                    # auto-detecta vectors.bin / .gz
res = fit_tree(X, y, max_depth=20, min_samples_leaf=50)
metrics = evaluate(res.clf, res.X_test, res.y_test)
save_model(res.clf, "fraud_dt.joblib")
```

## Pipeline do notebook

1. **Setup** — imports de `fraud_dt` + seaborn/matplotlib
2. **Load** — `vectors.bin` packed int16 (rápido) ou fallback `references.json.gz`
3. **EDA**
   - Schema + qualidade (NaN/Inf)
   - Balanço de classes
   - Análise do sentinela `-1` nos dims 5 e 6 (`minutes_since_last_tx`, `km_from_last_tx`)
   - KDE por classe das features-chave
   - Matriz de correlação
4. **Decisão de tratamento** — sentinela NÃO é removida/imputada (a spec é explícita; o KNN oracle agrupa transações sem histórico)
5. **Sweep de hiperparâmetros** — 3-fold CV em sub-amostra estratificada
6. **Treino final** — DT no melhor `max_depth`
7. **Baseline Random Forest** — 50 árvores, comparação direta
8. **Feature importance**
9. **Confusion + ROC + Precision-Recall**
10. **Visualização da árvore** (primeiros 3 níveis)
11. **Persistência** (`fraud_dt.joblib`)
12. **Reload + teste nos exemplos da spec** (`REGRAS_DE_DETECCAO.md`)
13. **Microbenchmark de inferência** (linha-a-linha, sem batching)

## Resultado da última run (DT, max_depth=20, min_samples_leaf=50)

```
accuracy=0.9826  f1=0.9738  auc≈0.99
TN=394867  FP=5252  FN=5207  TP=194674
inference: ~0.1µs/sample em batch
```

Top features por importance:
1. `amount_vs_avg`  (0.96)
2. `hour_of_day`    (0.03)
3. `merchant_avg_amount`
4. `minutes_since_last_tx`
5. `km_from_home`
