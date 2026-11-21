import { useState } from 'react';
import { categoryLabels, severityLabels, type IncidentDetail } from './model';
import type { IncidentFields } from './workflow-api';

// A pristine input follows the latest server value. Preserve local edits when a
// sibling mutation returns a newer incident, including an explicit empty value.
function useField<T extends string>(server: T) {
  const [base, setBase] = useState(server);
  const [value, setValue] = useState(server);
  if (base !== server) {
    setBase(server);
    if (value.trim() === base.trim() || value.trim() === server.trim()) setValue(server);
  }
  return [value, setValue, value.trim() !== server.trim()] as const;
}

export function ConfirmedFields({
  incident,
  disabled,
  save,
}: {
  incident: IncidentDetail;
  disabled: boolean;
  save: (patch: IncidentFields) => void;
}) {
  const [category, setCategory, categoryDirty] = useField(incident.category ?? '');
  const [severity, setSeverity, severityDirty] = useField(incident.severity ?? '');
  const [system, setSystem, systemDirty] = useField(incident.affected_system ?? '');
  const [resolution, setResolution, resolutionDirty] = useField(
    incident.resolution_note ?? '',
  );
  const patch: IncidentFields = {
    ...(categoryDirty && { category: category || null }),
    ...(severityDirty && { severity: severity || null }),
    ...(systemDirty && { affected_system: system.trim() || null }),
    ...(resolutionDirty && { resolution_note: resolution.trim() || null }),
  };
  const dirty = Object.keys(patch).length > 0;
  return (
    <form
      className="workflow-form"
      onSubmit={(event) => {
        event.preventDefault();
        if (dirty && !disabled) save(patch);
      }}
    >
      <h3>Edit confirmed fields</h3>
      <p className="muted">
        Category and severity are human decisions. The original report cannot be edited.
      </p>
      <div className="workflow-pair">
        <label>
          Category
          <select
            value={category}
            disabled={disabled}
            onChange={(e) => setCategory(e.target.value as typeof category)}
          >
            <option value="">Not assessed</option>
            {Object.entries(categoryLabels).map(([value, label]) => (
              <option value={value} key={value}>
                {label}
              </option>
            ))}
          </select>
        </label>
        <label>
          Severity
          <select
            value={severity}
            disabled={disabled}
            onChange={(e) => setSeverity(e.target.value as typeof severity)}
          >
            <option value="">Not assessed</option>
            {Object.entries(severityLabels).map(([value, label]) => (
              <option value={value} key={value}>
                {label}
              </option>
            ))}
          </select>
        </label>
      </div>
      <label>
        Affected system
        <input
          maxLength={500}
          value={system}
          disabled={disabled}
          onChange={(e) => setSystem(e.target.value)}
        />
      </label>
      <label>
        Resolution note
        <textarea
          rows={3}
          maxLength={10000}
          value={resolution}
          disabled={disabled}
          onChange={(e) => setResolution(e.target.value)}
        />
      </label>
      <button
        className="button button-secondary"
        type="submit"
        disabled={disabled || !dirty}
      >
        Save fields
      </button>
    </form>
  );
}
