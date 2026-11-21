import { useEffect, useState } from 'react';
import { useAuth } from '../auth/use-auth';
import { adminApi } from './workflow-api';
import { actionError } from './workflow-ui';

export function AssignmentPicker({
  disabled,
  currentId,
  assign,
}: {
  disabled: boolean;
  currentId: string | null;
  assign: (id: string) => void;
}) {
  const { runProtected } = useAuth();
  const [prefix, setPrefix] = useState('');
  const [query, setQuery] = useState({ prefix: '', cursor: null as string | null });
  const [page, setPage] = useState<Awaited<
    ReturnType<typeof adminApi.eligibleAnalysts>
  > | null>(null);
  const [error, setError] = useState('');
  const [selected, setSelected] = useState('');

  function search(cursor: string | null) {
    setSelected('');
    setPage(null);
    setError('');
    setQuery({ prefix: prefix.trim(), cursor });
  }
  useEffect(() => {
    const controller = new AbortController();
    void runProtected(() =>
      adminApi.eligibleAnalysts(query.prefix, query.cursor, controller.signal),
    ).then(
      (result) => {
        if (!controller.signal.aborted) setPage(result);
      },
      (err: unknown) => {
        if (!controller.signal.aborted) setError(actionError(err));
      },
    );
    return () => controller.abort();
  }, [query, runProtected]);

  const sameSearch = prefix.trim() === query.prefix;
  const canAssign =
    !disabled &&
    sameSearch &&
    selected &&
    selected !== currentId &&
    page?.users.some((user) => user.id === selected);
  return (
    <div className="workflow-form">
      <h3>Assign / reassign</h3>
      <form
        className="workflow-actions"
        onSubmit={(e) => {
          e.preventDefault();
          search(null);
        }}
      >
        <label>
          Username prefix
          <input
            type="search"
            value={prefix}
            onChange={(e) => setPrefix(e.target.value)}
          />
        </label>
        <button type="submit" className="button button-secondary">
          Search analysts
        </button>
      </form>
      {error ? (
        <p role="alert">{error}</p>
      ) : !page ? (
        <p role="status">Loading analysts…</p>
      ) : (
        <>
          <label>
            Active analyst
            <select
              disabled={disabled || !sameSearch}
              value={selected}
              onChange={(e) => setSelected(e.target.value)}
            >
              <option value="">Select analyst</option>
              {page.users.map((a) => (
                <option value={a.id} key={a.id}>
                  {a.username}
                </option>
              ))}
            </select>
          </label>
          {page.users.length === 0 && <p>No matching active analysts.</p>}
          {page.next_cursor && sameSearch && (
            <button
              type="button"
              className="button button-secondary"
              onClick={() => search(page.next_cursor)}
            >
              Next analysts
            </button>
          )}
          <button
            type="button"
            className="button button-secondary"
            disabled={!canAssign}
            onClick={() => {
              if (canAssign) assign(selected);
            }}
          >
            Save assignment
          </button>
        </>
      )}
    </div>
  );
}
