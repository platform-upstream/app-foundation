import React from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { App } from './App.js';
import { registerServiceWorker } from './foundation/register-service-worker.js';
// Foundation CSS (all in @layer foundation) first, the application's own CSS last, so it wins.
import './foundation/shell.css';
import './foundation/touch.css';
import './foundation/motion.css';
import './app.css';

// Query timing and retry behavior are application policy, so the template keeps them visible.
const queryClient = new QueryClient({
  defaultOptions: { queries: { staleTime: 15_000, retry: 1, refetchOnWindowFocus: true } },
});

const root = document.getElementById('root');
if (!root) throw new Error('Missing #root element');

createRoot(root).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <App />
    </QueryClientProvider>
  </React.StrictMode>,
);

// Production only: the update check compares the page's entry script with its own bundle, which
// never match under the Vite dev server, so the page would reload in a loop.
if (import.meta.env.PROD) registerServiceWorker();
