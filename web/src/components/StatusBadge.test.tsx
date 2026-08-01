import { render, screen } from '@testing-library/react';
import { ChakraProvider } from '@chakra-ui/react';
import { describe, it, expect } from 'vitest';
import { StatusBadge } from './StatusBadge';

function renderWithProviders(ui: React.ReactElement) {
  return render(<ChakraProvider>{ui}</ChakraProvider>);
}

describe('StatusBadge', () => {
  it('renders "on" state with green color', () => {
    renderWithProviders(<StatusBadge state="on" />);
    const badge = screen.getByText('on');
    expect(badge).toBeInTheDocument();
  });

  it('renders "off" state', () => {
    renderWithProviders(<StatusBadge state="off" />);
    expect(screen.getByText('off')).toBeInTheDocument();
  });

  it('renders "blocked" state', () => {
    renderWithProviders(<StatusBadge state="blocked" />);
    expect(screen.getByText('blocked')).toBeInTheDocument();
  });

  it('renders "unmanaged" state', () => {
    renderWithProviders(<StatusBadge state="unmanaged" />);
    expect(screen.getByText('unmanaged')).toBeInTheDocument();
  });

  it('has data-testid attribute', () => {
    renderWithProviders(<StatusBadge state="on" />);
    expect(screen.getByTestId('status-badge-on')).toBeInTheDocument();
  });
});
