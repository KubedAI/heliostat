'use client';

import { PageHeader } from '@/components/page-header';
import { useApi } from '@/lib/client/use-api';
import type { Model } from '@/lib/domain/types';

const ACCENTS = ['var(--blue)', 'var(--coral)', 'var(--accent)', 'var(--mint)'];

export function ModelsView() {
  const { data, error, loading } = useApi<{ items: Model[] }>('/api/models', 60_000);
  const models = data?.items ?? [];
  return (
    <>
      <PageHeader
        eyebrow="Platform catalog"
        title="Approved models"
        lede="Models approved by the platform team, with the GPUs each supports."
      />
      {error && <div className="notice error">Could not load the model catalog: {error}</div>}
      {!error && !loading && models.length === 0 && (
        <div className="notice info">
          The catalog is empty. Add models to the Helm values under models.
        </div>
      )}
      {models.length > 0 && (
        <div className="model-grid">
          {models.map((model, index) => (
            <article
              key={model.id}
              className="model-card"
              style={{ '--model-color': ACCENTS[index % ACCENTS.length] } as React.CSSProperties}
            >
              <p className="eyebrow">{model.provider}</p>
              <h2>{model.name}</h2>
              <p>{model.description}</p>
              <div className="chips">
                {model.tags.map((tag) => (
                  <span key={tag}>{tag}</span>
                ))}
              </div>
              <dl>
                <dt>Model ID</dt>
                <dd>{model.id}</dd>
                <dt>Task</dt>
                <dd>{model.task}</dd>
                <dt>Revision</dt>
                <dd>{model.revision}</dd>
                <dt>Context</dt>
                <dd>
                  {model.contextLength ? `${model.contextLength.toLocaleString()} tokens` : '—'}
                </dd>
                <dt>GPU profiles</dt>
                <dd>{model.gpuProfiles.join(', ') || '—'}</dd>
              </dl>
            </article>
          ))}
        </div>
      )}
    </>
  );
}
