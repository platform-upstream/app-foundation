import { FormEvent, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { z } from 'zod';
import { createJSONClient } from './foundation/api.js';

const ItemSchema = z.object({
  id: z.number().int(),
  name: z.string(),
  state: z.enum(['active', 'done']),
  createdAt: z.string(),
});
const StateSchema = z.object({
  items: z.array(ItemSchema),
  memory: z.object({ configured: z.boolean(), pending: z.number(), conflicts: z.number(), lastSync: z.string() }),
  llmEnabled: z.boolean(),
});
const AddSchema = z.object({ item: ItemSchema });
const CompletionSchema = z.object({ completion: z.string() });

const request = createJSONClient({ fallbackMessage: 'The reference request failed.' });

export function App() {
  const queryClient = useQueryClient();
  const [name, setName] = useState('');
  const [completion, setCompletion] = useState('');
  const state = useQuery({ queryKey: ['state'], queryFn: () => request('state', StateSchema) });
  const add = useMutation({
    mutationFn: (value: string) => request('items', AddSchema, { method: 'POST', body: JSON.stringify({ name: value }) }),
    onSuccess: async () => {
      setName('');
      await queryClient.invalidateQueries({ queryKey: ['state'] });
    },
  });
  const complete = useMutation({
    mutationFn: () => request('complete', CompletionSchema, {
      method: 'POST',
      body: JSON.stringify({ prompt: 'Reply with a short confirmation that the seam works.' }),
    }),
    onSuccess: (value) => setCompletion(value.completion),
  });

  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (name.trim()) add.mutate(name.trim());
  };

  return (
    <main>
      <header>
        <p className="eyebrow">Phase 1 proof</p>
        <h1>A small app with visible seams.</h1>
        <p className="intro">One Go process owns lifecycle, SQLite, authenticated JSON, and an embedded React PWA. Product design remains outside the foundation.</p>
      </header>

      <section aria-labelledby="data-title">
        <div className="section-heading">
          <div><p className="kicker">SQLite</p><h2 id="data-title">Temporary items</h2></div>
          <span className="status">{state.isPending ? 'Loading' : state.isError ? 'Unavailable' : `${state.data.items.length} stored`}</span>
        </div>
        <form onSubmit={submit}>
          <label htmlFor="item-name">Item name</label>
          <div className="input-row">
            <input id="item-name" value={name} onChange={(event) => setName(event.target.value)} maxLength={120} placeholder="Add a local-only item" />
            <button type="submit" disabled={add.isPending || !name.trim()}>Save item</button>
          </div>
        </form>
        {(state.error || add.error) && <p className="error" role="alert">{state.error?.message ?? add.error?.message}</p>}
        <ul className="items">
          {state.data?.items.map((item) => <li key={item.id}><span>{item.name}</span><small>{item.state}</small></li>)}
        </ul>
        {!state.isPending && !state.data?.items.length && <p className="empty">The isolated database starts empty.</p>}
      </section>

      <section aria-labelledby="seams-title">
        <div className="section-heading"><div><p className="kicker">Optional integrations</p><h2 id="seams-title">Failure-safe boundaries</h2></div></div>
        <div className="seam-grid">
          <article><strong>Memory</strong><span>{state.data?.memory.configured ? 'Configured' : 'Disabled cleanly'}</span></article>
          <article><strong>Local model</strong><span>{state.data?.llmEnabled ? 'Configured' : 'Disabled cleanly'}</span></article>
        </div>
        <button className="secondary" disabled={!state.data?.llmEnabled || complete.isPending} onClick={() => complete.mutate()}>Exercise model seam</button>
        {completion && <p className="completion">{completion}</p>}
        {complete.error && <p className="error" role="alert">{complete.error.message}</p>}
      </section>
    </main>
  );
}
