import { useState, type FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { z } from 'zod';
import { request } from '../../lib/api.js';
import { toast } from '../../foundation/toast.js';
import './notes.css';

// The scaffold's example feature: a round trip through the API and SQLite. Replace it.
const Note = z.object({ id: z.number(), text: z.string(), createdAt: z.string() });
const Notes = z.array(Note);
type Note = z.infer<typeof Note>;

const time = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' });

export function NotesPage() {
  const queryClient = useQueryClient();
  const [text, setText] = useState('');
  const notes = useQuery({ queryKey: ['notes'], queryFn: () => request('notes', Notes) });
  const add = useMutation({
    mutationFn: (value: string) => request('notes', Note, { method: 'POST', body: JSON.stringify({ text: value }) }),
    onSuccess: (note) => {
      queryClient.setQueryData<Note[]>(['notes'], (old = []) => [note, ...old]);
      setText('');
    },
  });
  const remove = useMutation({
    mutationFn: (id: number) => request(`notes/${id}`, z.unknown(), { method: 'DELETE' }),
    onSuccess: (_, id) => {
      queryClient.setQueryData<Note[]>(['notes'], (old = []) => old.filter((n) => n.id !== id));
      toast('Note deleted');
    },
    onError: (error) => toast(error.message, { tone: 'error' }),
  });

  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (text.trim() !== '') add.mutate(text);
  };

  return (
    <div className="page">
      <form className="note-form" onSubmit={submit}>
        <input
          aria-label="New note"
          placeholder="Write a note"
          value={text}
          maxLength={500}
          onChange={(event) => setText(event.target.value)}
        />
        <button type="submit" disabled={add.isPending || text.trim() === ''}>Add</button>
      </form>
      {add.error && <p role="alert" className="error">{add.error.message}</p>}
      {notes.isPending && <p className="muted">Loading…</p>}
      {notes.error && <p role="alert" className="error">{notes.error.message}</p>}
      {notes.data?.length === 0 && <p className="muted">No notes yet.</p>}
      <ul className="notes">
        {notes.data?.map((note) => (
          <li key={note.id}>
            <div>
              <p>{note.text}</p>
              <time dateTime={note.createdAt}>{time.format(new Date(note.createdAt))}</time>
            </div>
            <button type="button" onClick={() => remove.mutate(note.id)} disabled={remove.isPending}>Delete</button>
          </li>
        ))}
      </ul>
    </div>
  );
}
