export type GuacamoleInstructionHandler = (
  opcode: string,
  parameters: string[],
) => void;

export type GuacamoleInstructionElement = string | number | boolean;

const MAX_ELEMENT_CODEPOINTS = 1 << 20;
const MAX_INSTRUCTION_BUFFER = 8 << 20;
const MAX_ELEMENTS = 256;

function codeUnitEnd(
  value: string,
  start: number,
  codepoints: number,
): number {
  let index = start;
  for (let count = 0; count < codepoints; count += 1) {
    if (index >= value.length) return -1;
    const current = value.charCodeAt(index);
    if (current >= 0xD800 && current <= 0xDBFF) {
      if (index + 1 >= value.length) return -1;
      const next = value.charCodeAt(index + 1);
      index += next >= 0xDC00 && next <= 0xDFFF ? 2 : 1;
    } else {
      index += 1;
    }
  }
  return index;
}

export function guacamoleCodePointCount(value: string): number {
  let count = 0;
  for (const _codepoint of value) count += 1;
  return count;
}

export function encodeGuacamoleInstruction(
  elements: GuacamoleInstructionElement[],
): string {
  if (!elements.length || elements.length > MAX_ELEMENTS) {
    throw new Error('Guacamole instruction element count is invalid');
  }
  let encoded = '';
  elements.forEach((element, index) => {
    // Guacamole represents booleans as integers on the wire. In particular,
    // the key instruction requires pressed=1 and released=0. Stringifying a
    // JavaScript boolean would produce "true"/"false", which guacd does not
    // accept as a pressed-state integer.
    const value = typeof element === 'boolean'
      ? (element ? '1' : '0')
      : String(element);
    const length = guacamoleCodePointCount(value);
    if (length > MAX_ELEMENT_CODEPOINTS) {
      throw new Error('Guacamole instruction element is too large');
    }
    encoded += `${length}.${value}${index === elements.length - 1 ? ';' : ','}`;
    if (encoded.length > MAX_INSTRUCTION_BUFFER) {
      throw new Error('Guacamole instruction exceeds the browser buffer limit');
    }
  });
  return encoded;
}

/**
 * Streaming Guacamole parser with the Unicode codepoint handling added by
 * Apache Guacamole 1.6. The npm package currently stops at 1.5 and counts
 * UTF-16 code units, which breaks filenames and clipboard data containing
 * supplementary Unicode characters.
 */
export class UnicodeGuacamoleParser {
  oninstruction: GuacamoleInstructionHandler | null = null;

  private buffer = '';
  private elements: string[] = [];
  private instructionCodeUnits = 0;

  receive(packet: string): void {
    this.buffer += packet;
    if (this.buffer.length > MAX_INSTRUCTION_BUFFER) {
      throw new Error('Guacamole instruction exceeds the browser buffer limit');
    }

    while (this.buffer.length) {
      const lengthEnd = this.buffer.indexOf('.');
      if (lengthEnd < 0) {
        if (this.buffer.length > 10 || !/^\d*$/.test(this.buffer)) {
          throw new Error('Invalid Guacamole element length');
        }
        return;
      }

      const rawLength = this.buffer.slice(0, lengthEnd);
      if (!/^\d+$/.test(rawLength) || rawLength.length > 10) {
        throw new Error('Invalid Guacamole element length');
      }
      const length = Number(rawLength);
      if (!Number.isSafeInteger(length) || length > MAX_ELEMENT_CODEPOINTS) {
        throw new Error('Guacamole element exceeds the browser limit');
      }

      const valueStart = lengthEnd + 1;
      const valueEnd = codeUnitEnd(this.buffer, valueStart, length);
      if (valueEnd < 0 || valueEnd >= this.buffer.length) return;

      const terminator = this.buffer[valueEnd];
      if (terminator !== ',' && terminator !== ';') {
        throw new Error('Invalid Guacamole element terminator');
      }

      this.instructionCodeUnits += valueEnd + 1;
      if (this.instructionCodeUnits > MAX_INSTRUCTION_BUFFER) {
        throw new Error('Guacamole instruction exceeds the browser buffer limit');
      }
      this.elements.push(this.buffer.slice(valueStart, valueEnd));
      if (this.elements.length > MAX_ELEMENTS) {
        throw new Error('Guacamole instruction has too many elements');
      }
      this.buffer = this.buffer.slice(valueEnd + 1);

      if (terminator === ';') {
        const [opcode, ...parameters] = this.elements;
        this.elements = [];
        this.instructionCodeUnits = 0;
        if (!opcode) throw new Error('Guacamole instruction opcode is empty');
        this.oninstruction?.(opcode, parameters);
      }
    }
  }
}

type GuacamoleParserHost = {
  Parser: new () => UnicodeGuacamoleParser;
};

export function installUnicodeGuacamoleParser(guacamole: unknown): void {
  (guacamole as GuacamoleParserHost).Parser = UnicodeGuacamoleParser;
}
