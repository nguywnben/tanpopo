import {StrictMode} from 'react';
import {createRoot} from 'react-dom/client';
import '../assets/fonts/local-figtree-fonts.css';
import './styles/tokens.css';
import './styles/base.css';
import './styles/layout.css';
import './styles/components.css';
import {App} from './App';

const root = document.getElementById('root');
if (!root) {
  throw new Error('Tanpopo console mount element #root was not found.');
}

createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
